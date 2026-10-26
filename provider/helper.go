package provider

import (
	"errors"
	"fmt"

	schemaDataSource "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	schemaResource "github.com/hashicorp/terraform-plugin-framework/resource/schema"
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

func newReadOnlyDataSourceAttrFromResourceAttr(attr schemaResource.Attribute) schemaDataSource.Attribute {
	switch attr := attr.(type) {
	case schemaResource.StringAttribute:
		return schemaDataSource.StringAttribute{
			Computed:            true,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.BoolAttribute:
		return schemaDataSource.BoolAttribute{
			Computed:            true,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.Float32Attribute:
		return schemaDataSource.Float32Attribute{
			Computed:            true,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.Float64Attribute:
		return schemaDataSource.Float64Attribute{
			Computed:            true,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.Int32Attribute:
		return schemaDataSource.Int32Attribute{
			Computed:            true,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.Int64Attribute:
		return schemaDataSource.Int64Attribute{
			Computed:            true,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.NumberAttribute:
		return schemaDataSource.NumberAttribute{
			Computed:            true,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.DynamicAttribute:
		return schemaDataSource.DynamicAttribute{
			Computed:            true,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.ListAttribute:
		return schemaDataSource.ListAttribute{
			Computed:            true,
			ElementType:         attr.ElementType,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.SetAttribute:
		return schemaDataSource.SetAttribute{
			Computed:            true,
			ElementType:         attr.ElementType,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.ObjectAttribute:
		return schemaDataSource.ObjectAttribute{
			Computed:            true,
			AttributeTypes:      attr.AttributeTypes,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.MapAttribute:
		return schemaDataSource.MapAttribute{
			Computed:            true,
			ElementType:         attr.ElementType,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.SingleNestedAttribute:
		o := schemaDataSource.SingleNestedAttribute{
			Computed:            true,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
			CustomType:          attr.CustomType,
			Attributes:          make(map[string]schemaDataSource.Attribute, len(attr.Attributes)),
		}
		for k, v := range attr.Attributes {
			o.Attributes[k] = newReadOnlyDataSourceAttrFromResourceAttr(v)
		}
		return o

	case schemaResource.ListNestedAttribute:
		o := schemaDataSource.ListNestedAttribute{
			Computed:            true,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
			CustomType:          attr.CustomType,
			NestedObject: schemaDataSource.NestedAttributeObject{
				CustomType: attr.NestedObject.CustomType,
				Attributes: make(map[string]schemaDataSource.Attribute, len(attr.NestedObject.Attributes)),
			},
		}
		for k, v := range attr.NestedObject.Attributes {
			o.NestedObject.Attributes[k] = newReadOnlyDataSourceAttrFromResourceAttr(v)
		}
		return o

	case schemaResource.SetNestedAttribute:
		o := schemaDataSource.SetNestedAttribute{
			Computed:            true,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
			CustomType:          attr.CustomType,
			NestedObject: schemaDataSource.NestedAttributeObject{
				CustomType: attr.NestedObject.CustomType,
				Attributes: make(map[string]schemaDataSource.Attribute, len(attr.NestedObject.Attributes)),
			},
		}
		for k, v := range attr.NestedObject.Attributes {
			o.NestedObject.Attributes[k] = newReadOnlyDataSourceAttrFromResourceAttr(v)
		}
		return o

	case schemaResource.MapNestedAttribute:
		o := schemaDataSource.MapNestedAttribute{
			Computed:            true,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
			CustomType:          attr.CustomType,
			NestedObject: schemaDataSource.NestedAttributeObject{
				CustomType: attr.NestedObject.CustomType,
				Attributes: make(map[string]schemaDataSource.Attribute, len(attr.NestedObject.Attributes)),
			},
		}
		for k, v := range attr.NestedObject.Attributes {
			o.NestedObject.Attributes[k] = newReadOnlyDataSourceAttrFromResourceAttr(v)
		}
		return o

	default:
		return nil
	}
}
