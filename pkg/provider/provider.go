package provider

import (
	"crypto/sha256"

	"github.com/dollarshaveclub/terraform-provider-nrs/pkg/synthetics"
	"github.com/dollarshaveclub/terraform-provider-nrs/pkg/util"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/hashicorp/terraform/terraform"
	"github.com/pkg/errors"
)

// Provider returns a new New Relic Synthetics Terraform provider.
func Provider() terraform.ResourceProvider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
			"new_relic_api_key": &schema.Schema{
				Type:        schema.TypeString,
				Required:    true,
				Description: "An admin API key for New Relic",
				Sensitive:   true,
			},
		},
		ConfigureFunc: getClient,
		ResourcesMap: map[string]*schema.Resource{
			"nrs_monitor": NRSMonitorResource(),
		},
	}
}

func getClient(rd *schema.ResourceData) (interface{}, error) {
	apiKey, ok := rd.Get("new_relic_api_key").(string)
	if !ok {
		return nil, errors.New("invalid type for new relic api key")
	}

	conf := func(s *synthetics.Client) {
		s.APIKey = apiKey
	}
	client, err := synthetics.NewClient(conf)
	if err != nil {
		return nil, errors.Wrap(err, "error: could not instantiate synthetics client")
	}

	return client, nil
}

// NRSMonitorResource returns a Terraform schema for a New Relic
// Synthetics monitor.
func NRSMonitorResource() *schema.Resource {
	return &schema.Resource{
		Schema: map[string]*schema.Schema{
			"id": &schema.Schema{
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The monitor's ID with New Relic",
			},
			"name": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"frequency": &schema.Schema{
				Type:        schema.TypeInt,
				Required:    true,
				Description: "The monitor's checking frequency in minutes (one of 1, 5, 10, 15, 30, 60, 360, 720, or 1440)",
			},
			"uri": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The URL to monitor",
			},
			"locations": &schema.Schema{
				Type:        schema.TypeList,
				Required:    true,
				Description: "The locations to check from",
				Elem:        &schema.Schema{Type: schema.TypeString},
			},
			"status": &schema.Schema{
				Type:         schema.TypeString,
				Required:     true,
				InputDefault: "ENABLED",
				Description:  "The monitor's status (one of ENABLED, MUTED, DISABLED)",
			},
			"sla_threshold": &schema.Schema{
				Type:        schema.TypeFloat,
				Description: "The monitor's SLA threshold",
				Optional:    true,
			},
			"validation_string": &schema.Schema{
				Type:        schema.TypeString,
				Description: "The monitor's validation string",
				Optional:    true,
			},
			"verify_ssl": &schema.Schema{
				Type:        schema.TypeBool,
				Description: "Verify SSL",
				Optional:    true,
			},
			"bypass_head_request": &schema.Schema{
				Type:        schema.TypeBool,
				Description: "Bypass HEAD request",
				Optional:    true,
			},
			"treat_redirect_as_failure": &schema.Schema{
				Type:        schema.TypeBool,
				Description: "Treat redirect as failure",
				Optional:    true,
			},
			"script": &schema.Schema{
				Type:        schema.TypeString,
				Description: "The script to execute",
				Optional:    true,
				StateFunc:   sha256StateFunc,
			},
			"script_locations": &schema.Schema{
				Type:        schema.TypeList,
				Description: "The private locations to execute the script from",
				Optional:    true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": &schema.Schema{
							Type:        schema.TypeString,
							Description: "The name of the private location",
							Optional:    true,
						},
						"hmac": &schema.Schema{
							Type:        schema.TypeString,
							Description: "The HMAC for the private location",
							Optional:    true,
						},
					},
				},
			},
			"type": &schema.Schema{
				Type:        schema.TypeString,
				Description: "The type of monitor (one of SIMPLE, BROWSER, SCRIPT_API, SCRIPT_BROWSER)",
				Required:    true,
				ForceNew:    true,
			},
		},
		Create: NRSMonitorCreate,
		Exists: NRSMonitorExists,
		Delete: NRSMonitorDelete,
		Read:   NRSMonitorRead,
		Update: NRSMonitorUpdate,
	}
}

func sha256StateFunc(i interface{}) string {
	s := i.(string)
	hash := sha256.New()
	hash.Write([]byte(s))
	return string(hash.Sum(nil))
}

// NRSMonitorCreate creates a new Synthetics monitor using Terraform
// configuration.
func NRSMonitorCreate(resourceData *schema.ResourceData, meta interface{}) error {
	client := meta.(*synthetics.Client)

	args := &synthetics.CreateMonitorArgs{
		Name:         resourceData.Get("name").(string),
		Type:         resourceData.Get("type").(string),
		Frequency:    uint(resourceData.Get("frequency").(int)),
		URI:          resourceData.Get("uri").(string),
		Locations:    util.StrSlice(resourceData.Get("locations").([]interface{})),
		Status:       resourceData.Get("status").(string),
		SLAThreshold: resourceData.Get("sla_threshold").(float64),
	}

	if data, ok := resourceData.GetOk("validation_string"); ok {
		args.ValidationString = util.StrPtr(data.(string))
	}
	if data, ok := resourceData.GetOk("verify_ssl"); ok {
		args.VerifySSL = util.BoolPtr(data.(bool))
	}
	if data, ok := resourceData.GetOk("bypass_head_request"); ok {
		args.BypassHEADRequest = util.BoolPtr(data.(bool))
	}
	if data, ok := resourceData.GetOk("treat_redirect_as_failure"); ok {
		args.TreatRedirectAsFailure = util.BoolPtr(data.(bool))
	}

	monitor, err := client.CreateMonitor(args)
	if err != nil {
		return errors.Wrapf(err, "error: could not create monitor")
	}

	resourceData.SetId(monitor.ID)

	// Set script if it was provided.
	if data, ok := resourceData.GetOk("script"); ok {
		args := &synthetics.UpdateMonitorScriptArgs{
			ScriptText: data.(string),
		}

		// Set script locations
		if data, ok := resourceData.GetOk("script_locations"); ok {
			scriptLocations := data.([]map[string]interface{})
			for _, scriptLocation := range scriptLocations {
				args.ScriptLocations = append(
					args.ScriptLocations,
					&synthetics.ScriptLocation{
						Name: scriptLocation["name"].(string),
						HMAC: scriptLocation["hmac"].(string),
					},
				)
			}
		}

		if err := client.UpdateMonitorScript(monitor.ID, args); err != nil {
			return errors.Wrap(err, "error: could not update monitor script")
		}
	}

	return nil
}

// NRSMonitorUpdate updates a Synthetics monitor using Terraform
// configuration.
func NRSMonitorUpdate(resourceData *schema.ResourceData, meta interface{}) error {
	client := meta.(*synthetics.Client)

	args := &synthetics.UpdateMonitorArgs{
		Name:         resourceData.Get("name").(string),
		Frequency:    uint(resourceData.Get("frequency").(int)),
		URI:          resourceData.Get("uri").(string),
		Locations:    util.StrSlice(resourceData.Get("locations").([]interface{})),
		Status:       resourceData.Get("status").(string),
		SLAThreshold: resourceData.Get("sla_threshold").(float64),
	}

	if resourceData.HasChange("validation_string") {
		validationString := resourceData.Get("validation_string").(string)
		if validationString != "" {
			args.ValidationString = util.StrPtr(validationString)
		}
	}
	if resourceData.HasChange("verify_ssl") {
		args.VerifySSL = util.BoolPtr(resourceData.Get("verify_ssl").(bool))
	}
	if resourceData.HasChange("bypass_head_request") {
		args.BypassHEADRequest = util.BoolPtr(resourceData.Get("bypass_head_request").(bool))
	}
	if resourceData.HasChange("treat_redirect_as_failure") {
		args.TreatRedirectAsFailure = util.BoolPtr(resourceData.Get("treat_redirect_as_failure").(bool))
	}

	if _, err := client.UpdateMonitor(resourceData.Id(), args); err != nil {
		return errors.Wrapf(err, "error: could not update monitor")
	}

	if resourceData.HasChange("script") {
		script := resourceData.Get("script").(string)
		scriptArgs := &synthetics.UpdateMonitorScriptArgs{
			ScriptText: script,
		}

		if data, ok := resourceData.GetOk("script_locations"); ok {
			scriptLocations := data.([]map[string]interface{})
			for _, scriptLocation := range scriptLocations {
				scriptArgs.ScriptLocations = append(
					scriptArgs.ScriptLocations,
					&synthetics.ScriptLocation{
						Name: scriptLocation["name"].(string),
						HMAC: scriptLocation["hmac"].(string),
					},
				)
			}
		}

		if err := client.UpdateMonitorScript(resourceData.Id(), scriptArgs); err != nil {
			return errors.Wrapf(err, "error: could not update monitor script")
		}
	}

	return nil
}

// NRSMonitorRead updates Terraform configuration for a Synthetics monitor.
func NRSMonitorRead(resourceData *schema.ResourceData, meta interface{}) error {
	client := meta.(*synthetics.Client)

	monitor, err := client.GetMonitor(resourceData.Id())
	if err != nil {
		return errors.Wrap(err, "error: could not get monitor")
	}

	script, err := client.GetMonitorScript(resourceData.Id())
	switch err {
	case synthetics.ErrMonitorScriptNotFound:
		if err := resourceData.Set("script", nil); err != nil {
			return err
		}
		if err := resourceData.Set("script_locations", nil); err != nil {
			return err
		}
	case nil:
		if err := resourceData.Set("script", sha256StateFunc(script)); err != nil {
			return err
		}
	default:
		return errors.Wrap(err, "error: could not get monitor script")
	}

	if err := resourceData.Set("name", monitor.Name); err != nil {
		return err
	}
	if err := resourceData.Set("type", monitor.Type); err != nil {
		return err
	}
	if err := resourceData.Set("frequency", monitor.Frequency); err != nil {
		return err
	}
	if err := resourceData.Set("uri", monitor.URI); err != nil {
		return err
	}
	if err := resourceData.Set("locations", monitor.Locations); err != nil {
		return err
	}
	if err := resourceData.Set("status", monitor.Status); err != nil {
		return err
	}
	if err := resourceData.Set("sla_threshold", monitor.SLAThreshold); err != nil {
		return err
	}

	if monitor.ValidationString != nil {
		if err := resourceData.Set("validation_string", *monitor.ValidationString); err != nil {
			return err
		}
	} else {
		if err := resourceData.Set("validation_string", nil); err != nil {
			return err
		}
	}

	if monitor.VerifySSL != nil {
		if err := resourceData.Set("verify_ssl", *monitor.VerifySSL); err != nil {
			return err
		}
	} else {
		if err := resourceData.Set("verify_ssl", nil); err != nil {
			return err
		}
	}

	if monitor.BypassHEADRequest != nil {
		if err := resourceData.Set("bypass_head_request", *monitor.BypassHEADRequest); err != nil {
			return err
		}
	} else {
		if err := resourceData.Set("bypass_head_request", nil); err != nil {
			return err
		}
	}

	if monitor.TreatRedirectAsFailure != nil {
		if err := resourceData.Set("treat_redirect_as_failure", *monitor.TreatRedirectAsFailure); err != nil {
			return err
		}
	} else {
		if err := resourceData.Set("treat_redirect_as_failure", nil); err != nil {
			return err
		}
	}

	return nil
}

// NRSMonitorDelete deletes a Synthetics monitor using Terraform
// configuration.
func NRSMonitorDelete(resourceData *schema.ResourceData, meta interface{}) error {
	client := meta.(*synthetics.Client)

	if err := client.DeleteMonitor(resourceData.Id()); err != nil {
		return errors.Wrap(err, "error: could not delete monitor")
	}

	return nil
}

// NRSMonitorExists checks whether a Synthetics monitor exists.
func NRSMonitorExists(resourceData *schema.ResourceData, meta interface{}) (bool, error) {
	client := meta.(*synthetics.Client)

	if _, err := client.GetMonitor(resourceData.Id()); err != nil {
		if err == synthetics.ErrMonitorNotFound {
			return false, nil
		}
		return false, errors.Wrap(err, "error: could not get monitor")
	}

	return true, nil
}
