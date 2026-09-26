package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/require"
)

func TestAIGatewayDataSourceRegisteredWithFrameworkProvider(t *testing.T) {
	p := &frameworkProvider{}
	dataSources := p.DataSources(context.Background())

	var found datasource.DataSource
	for _, factory := range dataSources {
		candidate := factory()
		var metadata datasource.MetadataResponse
		candidate.Metadata(context.Background(), datasource.MetadataRequest{}, &metadata)
		if metadata.TypeName == "neon_ai_gateway" {
			found = candidate
			break
		}
	}

	require.NotNil(t, found, "neon_ai_gateway must be registered as a Framework data source")
}

func TestAIGatewayDataSourceSchema(t *testing.T) {
	d := NewAIGatewayDataSource()
	var response datasource.SchemaResponse
	d.Schema(context.Background(), datasource.SchemaRequest{}, &response)

	require.Zero(t, response.Diagnostics.ErrorsCount())
	require.Contains(t, response.Schema.Attributes, "project_id")
	require.Contains(t, response.Schema.Attributes, "branch_id")
	require.Contains(t, response.Schema.Attributes, "base_url")
	require.NotContains(t, response.Schema.Attributes, "enabled")
}

func TestAIGatewayDataSourceModelUsesFrameworkTypes(t *testing.T) {
	model := neonAIGatewayDataSourceModel{
		ID:        types.StringValue("project/branch"),
		ProjectID: types.StringValue("project"),
		BranchID:  types.StringValue("branch"),
		BaseURL:   types.StringValue("https://example.invalid"),
	}

	require.Equal(t, "project/branch", model.ID.ValueString())
}
