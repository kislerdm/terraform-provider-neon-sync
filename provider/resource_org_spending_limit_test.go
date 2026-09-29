package provider

import (
	"context"
	"encoding/json"
	"testing"

	frameworkresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

func TestNeonOrgSpendingLimitMetadata(t *testing.T) {
	spendingLimitResource := NewNeonOrgSpendingLimitResource()
	response := frameworkresource.MetadataResponse{}
	spendingLimitResource.Metadata(
		context.Background(),
		frameworkresource.MetadataRequest{},
		&response,
	)

	require.Equal(t, "neon_org_spending_limit", response.TypeName)
}

func TestNeonOrgSpendingLimitSchema(t *testing.T) {
	spendingLimitResource := NewNeonOrgSpendingLimitResource()
	response := frameworkresource.SchemaResponse{}
	spendingLimitResource.Schema(
		context.Background(),
		frameworkresource.SchemaRequest{},
		&response,
	)

	require.Contains(t, response.Schema.Attributes, "id")
	require.Contains(t, response.Schema.Attributes, "org_id")
	require.Contains(t, response.Schema.Attributes, "spending_limit_cents")
	require.NotContains(t, response.Schema.Attributes, "status")
	require.NotContains(t, response.Schema.Attributes, "usage")
}

func TestNeonOrgSpendingLimitModelIdentity(t *testing.T) {
	model := neonOrgSpendingLimitResourceModel{
		OrgID:              types.StringValue("org-123"),
		SpendingLimitCents: types.Int64Value(10000),
	}

	require.Equal(t, "org-123", spendingLimitResourceID(model.OrgID))
}

func TestNeonOrgSpendingLimitAbsentResponse(t *testing.T) {
	require.True(t, spendingLimitIsAbsent(0))
	require.False(t, spendingLimitIsAbsent(1))
}

func TestNeonOrgSpendingLimitSDKNullResponseDecodesAsAbsent(t *testing.T) {
	var response neon.SpendingLimitResponse
	err := json.Unmarshal([]byte(`{"spending_limit_cents":null}`), &response)
	require.NoError(t, err)
	require.True(t, spendingLimitIsAbsent(response.SpendingLimitCents))
}
