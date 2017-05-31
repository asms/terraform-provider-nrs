package synthetics

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"regexp"
	"time"

	"encoding/json"

	"strconv"

	"github.com/pkg/errors"
)

const (
	timeFormat = "2006-01-02T15:04:05.999999999-0700"
)

var (
	monitorURL = regexp.MustCompile(`^https://synthetics.newrelic.com/synthetics/api/v3/monitors/(.+)$`)
)

// HTTPClient is the interface to the HTTP clients that a Client can
// use.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client is a client to New Relic Synthetics.
type Client struct {
	APIKey     string
	HTTPClient HTTPClient
}

// NewClient instantiates a new Client.
func NewClient(configs ...func(*Client)) (*Client, error) {
	client := &Client{}

	for _, config := range configs {
		config(client)
	}

	// Validate configuration
	if client.APIKey == "" {
		return nil, errors.New("error: synthetics api key not provided")
	}
	if client.HTTPClient == nil {
		client.HTTPClient = http.DefaultClient
	}

	return client, nil
}

func (c *Client) getRequest(method, url string, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, errors.Wrap(err, "error: Synthetics request could not be created")
	}

	request.Header.Add("X-Api-Key", c.APIKey)

	return request, nil
}

// ExtendedMonitor is the monitor format provided by GetAllMonitors.
type ExtendedMonitor struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Type         string                 `json:"type"`
	Frequency    uint                   `json:"frequency"`
	URI          string                 `json:"uri"`
	Locations    []string               `json:"locations"`
	Status       string                 `json:"status"`
	SLAThreshold float64                `json:"slaThreshold"`
	Options      map[string]interface{} `json:"options"`
	ModifiedAt   time.Time
	CreatedAt    time.Time
	UserID       uint   `json:"userId"`
	APIVersion   string `json:"apiVersion"`

	// These are only used for parsing.
	ModifiedAtRaw string `json:"modifiedAt"`
	CreatedAtRaw  string `json:"createdAt"`
}

func (e *ExtendedMonitor) parse() error {
	var err error

	e.ModifiedAt, err = time.Parse(timeFormat, e.ModifiedAtRaw)
	if err != nil {
		return errors.Wrapf(err, "error: could not parse timestamp: %s", e.ModifiedAtRaw)
	}

	e.CreatedAt, err = time.Parse(timeFormat, e.CreatedAtRaw)
	if err != nil {
		return errors.Wrapf(err, "error: could not parse timestamp: %s", e.CreatedAtRaw)
	}

	return nil

}

// GetAllMonitorsArgs are the arguments to GetAllMonitors.
type GetAllMonitorsArgs struct {
	Offset uint
	Limit  uint
}

// GetAllMonitorsResponse is the response by GetAllMonitors.
type GetAllMonitorsResponse struct {
	Monitors []*ExtendedMonitor `json:"monitors"`
	Count    uint               `json:"count"`
}

// GetAllMonitors returns all monitors within a New Relic Synthetics
// account.
func (c *Client) GetAllMonitors(configs ...func(*GetAllMonitorsArgs)) (*GetAllMonitorsResponse, error) {
	requestArgs := &GetAllMonitorsArgs{}
	for _, config := range configs {
		config(requestArgs)
	}

	request, err := c.getRequest(
		"GET",
		"https://synthetics.newrelic.com/synthetics/api/v3/monitors",
		nil,
	)
	if err != nil {
		return nil, errors.Wrap(err, "error: could not create GetAllMonitors request")
	}

	if requestArgs.Offset > 0 {
		request.Form.Add("offset", strconv.FormatUint(uint64(requestArgs.Offset), 10))
	}
	if requestArgs.Limit > 0 {
		request.Form.Add("limit", strconv.FormatUint(uint64(requestArgs.Limit), 10))
	}

	response, err := c.HTTPClient.Do(request)
	if err != nil {
		return nil, errors.Wrap(err, "error: could not perform GetAllMonitors request")
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(response.Body)

		return nil, errors.Errorf(
			"error: invalid response from GetAllMonitors with code %d. Message: %s",
			response.StatusCode,
			body,
		)
	}

	var getAllMonitorsResponse GetAllMonitorsResponse
	if err := json.NewDecoder(response.Body).Decode(&getAllMonitorsResponse); err != nil {
		return nil, errors.Wrap(err, "error: could not parse GetAllMonitors JSON response")
	}
	for _, monitor := range getAllMonitorsResponse.Monitors {
		if err := monitor.parse(); err != nil {
			return nil, errors.Wrapf(err, "error: could not parse monitor: %s", monitor.ID)
		}
	}

	return &getAllMonitorsResponse, nil
}

// Monitor describes a specific Synthetics monitor.
type Monitor struct {
	ID           string   `json:"id,omitempty"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Frequency    uint     `json:"frequency"`
	URI          string   `json:"uri"`
	Locations    []string `json:"locations"`
	Status       string   `json:"status"`
	SLAThreshold float64  `json:"slaThreshold"`
	UserID       uint     `json:"userId,omitempty"`
	APIVersion   string   `json:"apiVersion,omitempty"`
}

// GetMonitor returns a specific Monitor.
func (c *Client) GetMonitor(id string) (*Monitor, error) {
	if id == "" {
		return nil, errors.Errorf("error: invalid id provided: %s", id)
	}

	request, err := c.getRequest(
		"GET",
		fmt.Sprintf("https://synthetics.newrelic.com/synthetics/api/v3/monitors/%s", id),
		nil,
	)
	if err != nil {
		return nil, errors.Wrap(err, "error: could not create GetMonitor request")
	}

	response, err := c.HTTPClient.Do(request)
	if err != nil {
		return nil, errors.Wrap(err, "error: could not perform GetMonitor request")
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		return nil, errors.New("error: could not find monitor")
	}
	if response.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(response.Body)

		return nil, errors.Errorf(
			"error: invalid response from GetMonitor with code %d. Message: %s",
			response.StatusCode,
			body,
		)
	}

	var monitor Monitor
	if err := json.NewDecoder(response.Body).Decode(&monitor); err != nil {
		return nil, errors.Wrap(err, "error: could not parse GetMonitor JSON response")
	}

	return &monitor, nil
}

// CreateMonitorArgs are the arguments to CreateMonitor.
type CreateMonitorArgs struct {
	Name         string                 `json:"name"`
	Type         string                 `json:"type"`
	Frequency    uint                   `json:"frequency"`
	URI          string                 `json:"uri"`
	Locations    []string               `json:"locations"`
	Status       string                 `json:"status"`
	SLAThreshold float64                `json:"slaThreshold"`
	Options      map[string]interface{} `json:"options"`
}

// CreateMonitor creates a new Monitor.
func (c *Client) CreateMonitor(m *CreateMonitorArgs) (*Monitor, error) {
	reqBody := &bytes.Buffer{}
	if err := json.NewEncoder(reqBody).Encode(m); err != nil {
		return nil, errors.Wrapf(err, "error: could not JSON encode monitor: %s", m.Name)
	}

	request, err := c.getRequest(
		"POST",
		"https://synthetics.newrelic.com/synthetics/api/v3/monitors",
		reqBody,
	)
	if err != nil {
		return nil, errors.Wrap(err, "error: could not create CreateMonitor request")
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := c.HTTPClient.Do(request)
	if err != nil {
		return nil, errors.Wrap(err, "error: could not perform CreateMonitor request")
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusCreated {
		body, _ := ioutil.ReadAll(response.Body)

		return nil, errors.Errorf(
			"error: invalid response from CreateMonitor with code %d. Message: %s",
			response.StatusCode,
			body,
		)
	}

	// Extract ID from URL returned in "Location" header
	location := response.Header.Get("Location")
	matches := monitorURL.FindAllStringSubmatch(location, 1)
	if len(matches) == 0 {
		return nil, errors.Errorf("error: could not find an ID for monitor in location header")
	}
	id := matches[0][1]

	monitor, err := c.GetMonitor(id)
	if err != nil {
		return nil, errors.Wrapf(err, "error: could not get metadata for monitor: %s", id)
	}

	return monitor, nil
}