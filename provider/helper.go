package provider

import (
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	neon "github.com/kislerdm/neon-sdk-go"
)

func intValidationNotNegative(v interface{}, s string) (warn []string, errs []error) {
	if vv, ok := v.(int); ok && vv < 0 {
		errs = append(errs, errors.New(s+" must be not negative"))
	}
	return
}

var schemaRegionID = &schema.Schema{
	Type:        schema.TypeString,
	Optional:    true,
	Computed:    true,
	ForceNew:    true,
	Description: "Deployment region: https://neon.tech/docs/introduction/regions",
}

type t interface {
	bool | string | int | int32 | int64 | float64 | float32 | neon.PgVersion | neon.ComputeUnit | neon.Provisioner | neon.SuspendTimeoutSeconds
}

func pointer[V t](v V) *V {
	if fmt.Sprintf("%v", v) == "" {
		return nil
	}
	return &v
}

type complexID struct {
	ProjectID, BranchID, Name string
}

func (v complexID) toString() string {
	return v.ProjectID + "/" + v.BranchID + "/" + v.Name
}

func stringChanged(prior, planned types.String) bool {
	if prior.IsUnknown() || planned.IsUnknown() || prior.IsNull() || planned.IsNull() {
		return false
	}
	return prior.ValueString() != planned.ValueString()
}
