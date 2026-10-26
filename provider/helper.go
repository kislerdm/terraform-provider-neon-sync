package provider

import (
	"fmt"

	schemaDataSource "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	schemaResource "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	neon "github.com/kislerdm/neon-sdk-go"
)

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

func newReadOnlyResourceAttrFromResourceAttr(attr schemaResource.Attribute) schemaResource.Attribute {
	switch attr.(type) {
	case schemaResource.SingleNestedAttribute:
		attr := attr.(schemaResource.SingleNestedAttribute)
		o := schemaResource.SingleNestedAttribute{
			Computed:            true,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
			CustomType:          attr.CustomType,
		}
		for k, v := range attr.Attributes {
			o.Attributes[k] = newReadOnlyDataSourceAttrFromResourceAttr(v)
		}
		return o

	case schemaResource.ListNestedAttribute:
		attr := attr.(schemaResource.ListNestedAttribute)
		o := schemaResource.ListNestedAttribute{
			Computed:            true,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
			CustomType:          attr.CustomType,
			NestedObject: schemaResource.NestedAttributeObject{
				CustomType: attr.NestedObject.CustomType,
			},
		}
		for k, v := range attr.NestedObject.Attributes {
			o.NestedObject.Attributes[k] = newReadOnlyDataSourceAttrFromResourceAttr(v)
		}
		return o

	case schemaResource.SetNestedAttribute:
		attr := attr.(schemaResource.SetNestedAttribute)
		o := schemaResource.SetNestedAttribute{
			Computed:            true,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
			CustomType:          attr.CustomType,
			NestedObject: schemaResource.NestedAttributeObject{
				CustomType: attr.NestedObject.CustomType,
			},
		}
		for k, v := range attr.NestedObject.Attributes {
			o.NestedObject.Attributes[k] = newReadOnlyDataSourceAttrFromResourceAttr(v)
		}
		return o

	case schemaResource.MapNestedAttribute:
		attr := attr.(schemaResource.MapNestedAttribute)
		o := schemaResource.MapNestedAttribute{
			Computed:            true,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
			CustomType:          attr.CustomType,
			NestedObject: schemaResource.NestedAttributeObject{
				CustomType: attr.NestedObject.CustomType,
			},
		}
		for k, v := range attr.NestedObject.Attributes {
			o.NestedObject.Attributes[k] = newReadOnlyDataSourceAttrFromResourceAttr(v)
		}
		return o

	default:
		switch attr.GetType() {
		case basetypes.StringType{}:
			attr := attr.(schemaResource.StringAttribute)
			return schemaResource.StringAttribute{
				Computed:            true,
				CustomType:          attr.CustomType,
				Sensitive:           attr.Sensitive,
				Description:         attr.Description,
				MarkdownDescription: attr.MarkdownDescription,
				DeprecationMessage:  attr.DeprecationMessage,
			}

		case basetypes.BoolType{}:
			attr := attr.(schemaResource.BoolAttribute)
			return schemaResource.BoolAttribute{
				Computed:            true,
				CustomType:          attr.CustomType,
				Sensitive:           attr.Sensitive,
				Description:         attr.Description,
				MarkdownDescription: attr.MarkdownDescription,
				DeprecationMessage:  attr.DeprecationMessage,
			}

		case basetypes.Float32Type{}:
			attr := attr.(schemaResource.Float32Attribute)
			return schemaResource.Float32Attribute{
				Computed:            true,
				CustomType:          attr.CustomType,
				Sensitive:           attr.Sensitive,
				Description:         attr.Description,
				MarkdownDescription: attr.MarkdownDescription,
				DeprecationMessage:  attr.DeprecationMessage,
			}

		case basetypes.Float64Type{}:
			attr := attr.(schemaResource.Float64Attribute)
			return schemaResource.Float64Attribute{
				Computed:            true,
				CustomType:          attr.CustomType,
				Sensitive:           attr.Sensitive,
				Description:         attr.Description,
				MarkdownDescription: attr.MarkdownDescription,
				DeprecationMessage:  attr.DeprecationMessage,
			}

		case basetypes.Int32Type{}:
			attr := attr.(schemaResource.Int32Attribute)
			return schemaResource.Int32Attribute{
				Computed:            true,
				CustomType:          attr.CustomType,
				Sensitive:           attr.Sensitive,
				Description:         attr.Description,
				MarkdownDescription: attr.MarkdownDescription,
				DeprecationMessage:  attr.DeprecationMessage,
			}

		case basetypes.Int64Type{}:
			attr := attr.(schemaResource.Int64Attribute)
			return schemaResource.Int64Attribute{
				Computed:            true,
				CustomType:          attr.CustomType,
				Sensitive:           attr.Sensitive,
				Description:         attr.Description,
				MarkdownDescription: attr.MarkdownDescription,
				DeprecationMessage:  attr.DeprecationMessage,
			}

		case basetypes.ListType{}:
			attr := attr.(schemaResource.ListAttribute)
			return schemaResource.ListAttribute{
				Computed:            true,
				ElementType:         attr.ElementType,
				CustomType:          attr.CustomType,
				Sensitive:           attr.Sensitive,
				Description:         attr.Description,
				MarkdownDescription: attr.MarkdownDescription,
				DeprecationMessage:  attr.DeprecationMessage,
			}

		case basetypes.MapType{}:
			attr := attr.(schemaResource.MapAttribute)
			return schemaResource.MapAttribute{
				Computed:            true,
				ElementType:         attr.ElementType,
				CustomType:          attr.CustomType,
				Sensitive:           attr.Sensitive,
				Description:         attr.Description,
				MarkdownDescription: attr.MarkdownDescription,
				DeprecationMessage:  attr.DeprecationMessage,
			}

		case basetypes.NumberType{}:
			attr := attr.(schemaResource.NumberAttribute)
			return schemaResource.NumberAttribute{
				Computed:            true,
				CustomType:          attr.CustomType,
				Sensitive:           attr.Sensitive,
				Description:         attr.Description,
				MarkdownDescription: attr.MarkdownDescription,
				DeprecationMessage:  attr.DeprecationMessage,
			}

		case basetypes.SetType{}:
			attr := attr.(schemaResource.SetAttribute)
			return schemaResource.SetAttribute{
				Computed:            true,
				ElementType:         attr.ElementType,
				CustomType:          attr.CustomType,
				Sensitive:           attr.Sensitive,
				Description:         attr.Description,
				MarkdownDescription: attr.MarkdownDescription,
				DeprecationMessage:  attr.DeprecationMessage,
			}

		case basetypes.DynamicType{}:
			attr := attr.(schemaResource.DynamicAttribute)
			return schemaResource.DynamicAttribute{
				Computed:            true,
				CustomType:          attr.CustomType,
				Sensitive:           attr.Sensitive,
				Description:         attr.Description,
				MarkdownDescription: attr.MarkdownDescription,
				DeprecationMessage:  attr.DeprecationMessage,
			}
		default:
			attr, ok := attr.(schemaResource.ObjectAttribute)
			if ok {
				return schemaResource.ObjectAttribute{
					Computed:            true,
					AttributeTypes:      attr.AttributeTypes,
					CustomType:          attr.CustomType,
					Sensitive:           attr.Sensitive,
					Description:         attr.Description,
					MarkdownDescription: attr.MarkdownDescription,
					DeprecationMessage:  attr.DeprecationMessage,
				}
			}

			return nil
		}
	}
}

func newReadOnlyDataSourceAttrFromResourceAttr(attr schemaResource.Attribute) schemaDataSource.Attribute {
	switch attr.(type) {
	case schemaResource.SingleNestedAttribute:
		attr := attr.(schemaResource.SingleNestedAttribute)
		o := schemaDataSource.SingleNestedAttribute{
			Computed:            true,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
			CustomType:          attr.CustomType,
		}
		for k, v := range attr.Attributes {
			o.Attributes[k] = newReadOnlyDataSourceAttrFromResourceAttr(v)
		}
		return o

	case schemaResource.ListNestedAttribute:
		attr := attr.(schemaResource.ListNestedAttribute)
		o := schemaDataSource.ListNestedAttribute{
			Computed:            true,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
			CustomType:          attr.CustomType,
			NestedObject: schemaDataSource.NestedAttributeObject{
				CustomType: attr.NestedObject.CustomType,
			},
		}
		for k, v := range attr.NestedObject.Attributes {
			o.NestedObject.Attributes[k] = newReadOnlyDataSourceAttrFromResourceAttr(v)
		}
		return o

	case schemaResource.SetNestedAttribute:
		attr := attr.(schemaResource.SetNestedAttribute)
		o := schemaDataSource.SetNestedAttribute{
			Computed:            true,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
			CustomType:          attr.CustomType,
			NestedObject: schemaDataSource.NestedAttributeObject{
				CustomType: attr.NestedObject.CustomType,
			},
		}
		for k, v := range attr.NestedObject.Attributes {
			o.NestedObject.Attributes[k] = newReadOnlyDataSourceAttrFromResourceAttr(v)
		}
		return o

	case schemaResource.MapNestedAttribute:
		attr := attr.(schemaResource.MapNestedAttribute)
		o := schemaDataSource.MapNestedAttribute{
			Computed:            true,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
			CustomType:          attr.CustomType,
			NestedObject: schemaDataSource.NestedAttributeObject{
				CustomType: attr.NestedObject.CustomType,
			},
		}
		for k, v := range attr.NestedObject.Attributes {
			o.NestedObject.Attributes[k] = newReadOnlyDataSourceAttrFromResourceAttr(v)
		}
		return o

	default:
		switch attr.GetType() {
		case basetypes.StringType{}:
			attr := attr.(schemaResource.StringAttribute)
			return schemaDataSource.StringAttribute{
				Computed:            true,
				CustomType:          attr.CustomType,
				Sensitive:           attr.Sensitive,
				Description:         attr.Description,
				MarkdownDescription: attr.MarkdownDescription,
				DeprecationMessage:  attr.DeprecationMessage,
			}

		case basetypes.BoolType{}:
			attr := attr.(schemaResource.BoolAttribute)
			return schemaDataSource.BoolAttribute{
				Computed:            true,
				CustomType:          attr.CustomType,
				Sensitive:           attr.Sensitive,
				Description:         attr.Description,
				MarkdownDescription: attr.MarkdownDescription,
				DeprecationMessage:  attr.DeprecationMessage,
			}

		case basetypes.Float32Type{}:
			attr := attr.(schemaResource.Float32Attribute)
			return schemaDataSource.Float32Attribute{
				Computed:            true,
				CustomType:          attr.CustomType,
				Sensitive:           attr.Sensitive,
				Description:         attr.Description,
				MarkdownDescription: attr.MarkdownDescription,
				DeprecationMessage:  attr.DeprecationMessage,
			}

		case basetypes.Float64Type{}:
			attr := attr.(schemaResource.Float64Attribute)
			return schemaDataSource.Float64Attribute{
				Computed:            true,
				CustomType:          attr.CustomType,
				Sensitive:           attr.Sensitive,
				Description:         attr.Description,
				MarkdownDescription: attr.MarkdownDescription,
				DeprecationMessage:  attr.DeprecationMessage,
			}

		case basetypes.Int32Type{}:
			attr := attr.(schemaResource.Int32Attribute)
			return schemaDataSource.Int32Attribute{
				Computed:            true,
				CustomType:          attr.CustomType,
				Sensitive:           attr.Sensitive,
				Description:         attr.Description,
				MarkdownDescription: attr.MarkdownDescription,
				DeprecationMessage:  attr.DeprecationMessage,
			}

		case basetypes.Int64Type{}:
			attr := attr.(schemaResource.Int64Attribute)
			return schemaDataSource.Int64Attribute{
				Computed:            true,
				CustomType:          attr.CustomType,
				Sensitive:           attr.Sensitive,
				Description:         attr.Description,
				MarkdownDescription: attr.MarkdownDescription,
				DeprecationMessage:  attr.DeprecationMessage,
			}

		case basetypes.ListType{}:
			attr := attr.(schemaResource.ListAttribute)
			return schemaDataSource.ListAttribute{
				Computed:            true,
				ElementType:         attr.ElementType,
				CustomType:          attr.CustomType,
				Sensitive:           attr.Sensitive,
				Description:         attr.Description,
				MarkdownDescription: attr.MarkdownDescription,
				DeprecationMessage:  attr.DeprecationMessage,
			}

		case basetypes.MapType{}:
			attr := attr.(schemaResource.MapAttribute)
			return schemaDataSource.MapAttribute{
				Computed:            true,
				ElementType:         attr.ElementType,
				CustomType:          attr.CustomType,
				Sensitive:           attr.Sensitive,
				Description:         attr.Description,
				MarkdownDescription: attr.MarkdownDescription,
				DeprecationMessage:  attr.DeprecationMessage,
			}

		case basetypes.NumberType{}:
			attr := attr.(schemaResource.NumberAttribute)
			return schemaDataSource.NumberAttribute{
				Computed:            true,
				CustomType:          attr.CustomType,
				Sensitive:           attr.Sensitive,
				Description:         attr.Description,
				MarkdownDescription: attr.MarkdownDescription,
				DeprecationMessage:  attr.DeprecationMessage,
			}

		case basetypes.SetType{}:
			attr := attr.(schemaResource.SetAttribute)
			return schemaDataSource.SetAttribute{
				Computed:            true,
				ElementType:         attr.ElementType,
				CustomType:          attr.CustomType,
				Sensitive:           attr.Sensitive,
				Description:         attr.Description,
				MarkdownDescription: attr.MarkdownDescription,
				DeprecationMessage:  attr.DeprecationMessage,
			}

		case basetypes.DynamicType{}:
			attr := attr.(schemaResource.DynamicAttribute)
			return schemaDataSource.DynamicAttribute{
				Computed:            true,
				CustomType:          attr.CustomType,
				Sensitive:           attr.Sensitive,
				Description:         attr.Description,
				MarkdownDescription: attr.MarkdownDescription,
				DeprecationMessage:  attr.DeprecationMessage,
			}
		default:
			attr, ok := attr.(schemaResource.ObjectAttribute)
			if ok {
				return schemaDataSource.ObjectAttribute{
					Computed:            true,
					AttributeTypes:      attr.AttributeTypes,
					CustomType:          attr.CustomType,
					Sensitive:           attr.Sensitive,
					Description:         attr.Description,
					MarkdownDescription: attr.MarkdownDescription,
					DeprecationMessage:  attr.DeprecationMessage,
				}
			}
			return nil
		}
	}
}
