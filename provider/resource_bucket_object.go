package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ resource.ResourceWithConfigure = (*neonBucketObjectResource)(nil)
var _ resource.ResourceWithImportState = (*neonBucketObjectResource)(nil)
var _ resource.ResourceWithModifyPlan = (*neonBucketObjectResource)(nil)

type neonBucketObjectResource struct {
	client *providerAdapter
}

type neonBucketObjectResourceModel struct {
	ID            types.String `tfsdk:"id"`
	ProjectID     types.String `tfsdk:"project_id"`
	BranchID      types.String `tfsdk:"branch_id"`
	Bucket        types.String `tfsdk:"bucket"`
	Key           types.String `tfsdk:"key"`
	Content       types.String `tfsdk:"content"`
	ContentBase64 types.String `tfsdk:"content_base64"`
	Source        types.String `tfsdk:"source"`
	IsDirectory   types.Bool   `tfsdk:"is_directory"`
	ContentType   types.String `tfsdk:"content_type"`
	ETag          types.String `tfsdk:"etag"`
	ContentLength types.Int64  `tfsdk:"content_length"`
	Trigger       types.String `tfsdk:"trigger"`
}

func NewNeonBucketObjectResource() resource.Resource {
	return &neonBucketObjectResource{}
}

func (r *neonBucketObjectResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_bucket_object"
}

func (r *neonBucketObjectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplaceString := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Manages an object in a Neon branchable object storage bucket.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: `Object state ID: <project_id>/<branch_id>/<bucket>/<key>.`,
			},
			"project_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplaceString,
				Description:   "The Neon project ID",
			},
			"branch_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplaceString,
				Description:   "The Neon branch ID",
			},
			"bucket": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplaceString,
				Description:   "The Neon object storage bucket name",
			},
			"key": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplaceString,
				Description:   "The object key.",
			},
			"content": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: `The content of the object. 
Note that it's not persisted in the Terraform state.
It **conflicts** with "content_base64", "source".`,
			},
			"content_base64": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: `The base64-encoded content of the object. 
Note that it's not persisted in the Terraform state. 
It **conflicts** with "content" and "source".`,
			},
			"source": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: `The absolute path to the the object's content.
It **conflicts** with "content" and "content_base64".`,
			},
			"content_type": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "The MIME type of the object.",
			},
			"etag": schema.StringAttribute{
				Computed:    true,
				Description: "The object's entity tag (content hash).",
			},
			"content_length": schema.Int64Attribute{
				Computed:    true,
				Description: "The object size in bytes.",
			},
			"trigger": schema.StringAttribute{
				Computed:      true,
				Optional:      true,
				PlanModifiers: requiresReplaceString,
				Description: `The provider-computed md5 check sum of the object, or user-provided string 
that is used as a trigger to re-upload the object.`,
			},
			"is_directory": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
				Description: `Set to true to provision a "folder". Note that it **conflicts** with "content", "content_base64", "source".`,
			},
		},
	}
}

func (r *neonBucketObjectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*providerAdapter)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			"Expected providerAdapter, got an unexpected type.")
		return
	}
	r.client = client
}

func (r *neonBucketObjectResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest,
	resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || req.Config.Raw.IsNull() {
		return
	}

	var planned neonBucketObjectResourceModel
	var config neonBucketObjectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &planned)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	contentDefined := !config.Content.IsNull() && !config.Content.IsUnknown()
	contentBase64Defined := !config.ContentBase64.IsNull() && !config.ContentBase64.IsUnknown()
	sourceDefined := !config.Source.IsNull() && !config.Source.IsUnknown()
	isDir := !config.IsDirectory.IsNull() && !config.IsDirectory.IsUnknown() && config.IsDirectory.ValueBool()
	if config.IsDirectory.IsNull() || config.IsDirectory.IsUnknown() {
		isDir = !planned.IsDirectory.IsNull() && !planned.IsDirectory.IsUnknown() && planned.IsDirectory.ValueBool()
	}

	switch {
	case contentDefined && contentBase64Defined && sourceDefined:
		resp.Diagnostics.AddError("conflicting configuration",
			`"content", "content_base64" and "source" cannot be specified together`)
	case contentDefined && contentBase64Defined:
		resp.Diagnostics.AddError("conflicting configuration",
			`"content" and "content_base64" cannot be specified together`)
	case contentDefined && sourceDefined:
		resp.Diagnostics.AddError("conflicting configuration",
			`"content" and "source" cannot be specified together`)
	case contentBase64Defined && sourceDefined:
		resp.Diagnostics.AddError("conflicting configuration",
			`"content_base64" and "source" cannot be specified together`)
	case isDir && (contentDefined || contentBase64Defined || sourceDefined):
		resp.Diagnostics.AddError("conflicting configuration",
			`"is_directory" cannot be specified with "content", "content_base64" or "source"`)
	case !isDir && !contentDefined && !contentBase64Defined && !sourceDefined:
		resp.Diagnostics.AddError("conflicting configuration",
			`either of the attributes must be provided: 
"is_directory", or "content", or "content_base64" or "source"`)
	}
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *neonBucketObjectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var plan neonBucketObjectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	objectKey, content, contentType, err := bucketObjectInput(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Bucket Object Input", err.Error())
		return
	}
	if err := r.upload(ctx, &plan, objectKey, content, contentType); err != nil {
		resp.Diagnostics.AddError("Unable to Upload Bucket Object", err.Error())
		return
	}

	plan.ID = types.StringValue(bucketObjectID(plan.ProjectID.ValueString(), plan.BranchID.ValueString(), plan.Bucket.ValueString(), plan.Key.ValueString()))
	// Do not persist any source content in Terraform state.
	plan.Content = types.StringNull()
	plan.ContentBase64 = types.StringNull()
	plan.Source = types.StringNull()
	if err := r.refresh(ctx, &plan, objectKey); err != nil {
		resp.Diagnostics.AddError("Unable to Read Uploaded Bucket Object", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *neonBucketObjectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonBucketObjectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	objectKey := state.Key.ValueString()
	if err := r.refresh(ctx, &state, objectKey); err != nil {
		if errors.Is(err, errBucketObjectNotFound) || isNeonNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to Read Bucket Object", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonBucketObjectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan neonBucketObjectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	objectKey, content, contentType, err := bucketObjectInput(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Bucket Object Input", err.Error())
		return
	}
	if err := r.upload(ctx, &plan, objectKey, content, contentType); err != nil {
		resp.Diagnostics.AddError("Unable to Upload Bucket Object", err.Error())
		return
	}
	plan.Content = types.StringNull()
	plan.ContentBase64 = types.StringNull()
	plan.Source = types.StringNull()
	if err := r.refresh(ctx, &plan, objectKey); err != nil {
		resp.Diagnostics.AddError("Unable to Read Uploaded Bucket Object", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *neonBucketObjectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}
	var state neonBucketObjectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	objectKey := state.Key.ValueString()
	if _, err := r.findObject(ctx, &state, objectKey); err != nil {
		placeholderKey := folderObjectKey(objectKey)
		if _, placeholderErr := r.findObject(ctx, &state, placeholderKey); placeholderErr != nil {
			if errors.Is(placeholderErr, errBucketObjectNotFound) {
				resp.State.RemoveResource(ctx)
				return
			}
			resp.Diagnostics.AddError("Unable to Find Bucket Object", placeholderErr.Error())
			return
		}
		objectKey = placeholderKey
	}
	resp.Diagnostics.Append(projectReadiness.RetryWithFallbackFramework(func(_ context.Context) error {
		return r.client.sdk.DeleteProjectBranchBucketObject(state.ProjectID.ValueString(), state.BranchID.ValueString(), state.Bucket.ValueString(), encodedObjectKey(objectKey))
	}, ctx, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
	})...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *neonBucketObjectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) < 4 {
		resp.Diagnostics.AddError("Invalid Neon Bucket Object Import ID", "Expected <project_id>/<branch_id>/<bucket>/<key>.")
		return
	}
	state := neonBucketObjectResourceModel{
		ID: types.StringValue(req.ID), ProjectID: types.StringValue(parts[0]), BranchID: types.StringValue(parts[1]),
		Bucket: types.StringValue(parts[2]), Key: types.StringValue(strings.Join(parts[3:], "/")),
		Content: types.StringNull(), ContentBase64: types.StringNull(), Source: types.StringNull(),
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}
	if err := r.refresh(ctx, &state, state.Key.ValueString()); err != nil {
		if err = r.refresh(ctx, &state, folderObjectKey(state.Key.ValueString())); err != nil {
			resp.Diagnostics.AddError("Bucket Object Not Found", err.Error())
			return
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonBucketObjectResource) upload(ctx context.Context, model *neonBucketObjectResourceModel, objectKey string, content []byte, contentType *string) error {
	cfg := neon.PresignRequest{
		Operation:   neon.PresignRequestOperationUpload,
		ContentType: contentType,
	}
	presigned, err := r.client.sdk.PresignProjectBranchBucketObject(model.ProjectID.ValueString(), model.BranchID.ValueString(), model.Bucket.ValueString(), encodedObjectKey(objectKey), cfg)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, presigned.Method, presigned.URL, bytes.NewReader(content))
	if err != nil {
		return err
	}
	for key, value := range presigned.Headers {
		req.Header.Set(key, fmt.Sprint(value))
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("presigned upload returned %s: %s", res.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (r *neonBucketObjectResource) refresh(ctx context.Context, model *neonBucketObjectResourceModel, objectKey string) error {
	object, err := r.findObject(ctx, model, objectKey)
	if err != nil {
		return err
	}

	cfg := neon.PresignRequest{Operation: neon.PresignRequestOperationDownload}
	presigned, err := r.client.sdk.PresignProjectBranchBucketObject(model.ProjectID.ValueString(), model.BranchID.ValueString(), model.Bucket.ValueString(), encodedObjectKey(objectKey), cfg)
	if err != nil {
		return err
	}
	if etag := presignedHeader(presigned.Headers, "ETag"); etag != "" {
		model.ETag = types.StringValue(etag)
	} else {
		model.ETag = types.StringValue(object.Etag)
	}
	if contentLength := presignedHeader(presigned.Headers, "Content-Length"); contentLength != "" {
		value, err := strconv.ParseInt(contentLength, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid Content-Length header %q: %w", contentLength, err)
		}
		model.ContentLength = types.Int64Value(value)
	} else {
		model.ContentLength = types.Int64Value(object.Size)
	}
	if contentType := presignedHeader(presigned.Headers, "Content-Type"); contentType != "" {
		model.ContentType = types.StringValue(contentType)
	} else {
		model.ContentType = types.StringNull()
	}
	return nil
}

func presignedHeader(headers map[string]any, name string) string {
	for header, value := range headers {
		if strings.EqualFold(header, name) {
			return fmt.Sprint(value)
		}
	}
	return ""
}

var errBucketObjectNotFound = errors.New("bucket object not found")

func (r *neonBucketObjectResource) findObject(ctx context.Context, model *neonBucketObjectResourceModel, objectKey string) (*neon.BucketObject, error) {
	var cursor *string
	for {
		result, err := r.client.sdk.ListProjectBranchBucketObjects(model.ProjectID.ValueString(), model.BranchID.ValueString(), model.Bucket.ValueString(), nil, nil, cursor, nil)
		if err != nil {
			return nil, err
		}
		for _, object := range result.Objects {
			if object.Key == objectKey {
				return &object, nil
			}
		}
		if !result.IsTruncated || result.NextCursor == nil {
			break
		}
		cursor = result.NextCursor
	}
	return nil, fmt.Errorf("%w: %q", errBucketObjectNotFound, objectKey)
}

func bucketObjectInput(model neonBucketObjectResourceModel) (string, []byte, *string, error) {
	inputs := 0
	if !model.Content.IsNull() && !model.Content.IsUnknown() {
		inputs++
	}
	if !model.ContentBase64.IsNull() && !model.ContentBase64.IsUnknown() {
		inputs++
	}
	if !model.Source.IsNull() && !model.Source.IsUnknown() {
		inputs++
	}
	if inputs > 1 {
		return "", nil, nil, fmt.Errorf("only one of content, content_base64, or source may be set")
	}
	contentType := model.ContentType.ValueStringPointer()
	if inputs == 0 {
		return folderObjectKey(model.Key.ValueString()), []byte{}, contentType, nil
	}
	if !model.Content.IsNull() && !model.Content.IsUnknown() {
		return model.Key.ValueString(), []byte(model.Content.ValueString()), contentType, nil
	}
	if !model.ContentBase64.IsNull() && !model.ContentBase64.IsUnknown() {
		decoded, err := base64.StdEncoding.DecodeString(model.ContentBase64.ValueString())
		return model.Key.ValueString(), decoded, contentType, err
	}
	content, err := os.ReadFile(model.Source.ValueString())
	return model.Key.ValueString(), content, contentType, err
}

func folderObjectKey(key string) string { return key + "/.emptyFolderPlaceholder" }

func encodedObjectKey(key string) string { return url.PathEscape(key) }

func bucketObjectID(projectID, branchID, bucket, key string) string {
	return strings.Join([]string{projectID, branchID, bucket, key}, "/")
}

func isNeonNotFound(err error) bool {
	var neonErr neon.Error
	return err != nil && errors.As(err, &neonErr) && neonErr.HTTPCode == http.StatusNotFound
}
