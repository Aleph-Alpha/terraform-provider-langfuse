package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/langfuse/terraform-provider-langfuse/internal/langfuse"
)

var _ resource.Resource = &userResource{}
var _ resource.ResourceWithImportState = &userResource{}

func NewUserResource() resource.Resource {
	return &userResource{}
}

type userResourceModel struct {
	ID                     types.String `tfsdk:"id"`
	Email                  types.String `tfsdk:"email"`
	UserName               types.String `tfsdk:"user_name"`
	Active                 types.Bool   `tfsdk:"active"`
	OrganizationPublicKey  types.String `tfsdk:"organization_public_key"`
	OrganizationPrivateKey types.String `tfsdk:"organization_private_key"`
	IgnoreDestroy          types.Bool   `tfsdk:"ignore_destroy"`
}

type userResource struct {
	ClientFactory langfuse.ClientFactory
}

func (r *userResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	clientFactory, ok := req.ProviderData.(langfuse.ClientFactory)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected langfuse.ClientFactory, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.ClientFactory = clientFactory
}

func (r *userResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (r *userResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Langfuse user via the SCIM API.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The unique identifier of the user (SCIM ID).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"email": schema.StringAttribute{
				Required:    true,
				Description: "The email address of the user (used as SCIM userName).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"user_name": schema.StringAttribute{
				Computed:    true,
				Description: "The SCIM userName (same as email).",
			},
			"active": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether the user account is active. Defaults to true.",
				Default:     booldefault.StaticBool(true),
			},
			"organization_public_key": schema.StringAttribute{
				Required:    true,
				Sensitive:   true,
				Description: "Organization public key to authenticate the call.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"organization_private_key": schema.StringAttribute{
				Required:    true,
				Sensitive:   true,
				Description: "Organization private key to authenticate the call.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"ignore_destroy": schema.BoolAttribute{
				Optional:    true,
				Description: "When true, the user will not be deleted from Langfuse when the resource is destroyed. Defaults to false.",
			},
		},
	}
}

func (r *userResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan userResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	organizationClient := r.ClientFactory.NewOrganizationClient(
		plan.OrganizationPublicKey.ValueString(),
		plan.OrganizationPrivateKey.ValueString(),
	)

	email := plan.Email.ValueString()
	active := true
	if !plan.Active.IsNull() && !plan.Active.IsUnknown() {
		active = plan.Active.ValueBool()
	}

	scimReq := &langfuse.SCIMUserRequest{
		Schemas:  []string{"urn:ietf:params:scim:schemas:core:2.0:User"},
		UserName: email,
		Active:   active,
		Emails: []struct {
			Value   string `json:"value"`
			Primary bool   `json:"primary"`
		}{
			{Value: email, Primary: true},
		},
	}

	user, err := organizationClient.CreateSCIMUser(ctx, scimReq)
	if err != nil {
		resp.Diagnostics.AddError("Error creating user", err.Error())
		return
	}

	plan.ID = types.StringValue(user.ID)
	plan.UserName = types.StringValue(user.UserName)
	// Do not overwrite plan.Active from the create response: Langfuse's SCIM
	// endpoint omits the "active" field in its response body, which causes Go's
	// JSON decoder to zero-value it (false). Keeping the planned value avoids a
	// spurious "provider produced inconsistent result" error. The Read function
	// will sync the real value on the next refresh.

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *userResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state userResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	organizationClient := r.ClientFactory.NewOrganizationClient(
		state.OrganizationPublicKey.ValueString(),
		state.OrganizationPrivateKey.ValueString(),
	)

	user, err := organizationClient.FindSCIMUserByEmail(ctx, state.Email.ValueString())
	if err != nil {
		if strings.Contains(err.Error(), "cannot find user") {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading user", err.Error())
		return
	}

	email := state.Email.ValueString()
	for _, e := range user.Emails {
		if e.Primary {
			email = e.Value
			break
		}
	}

	state.ID = types.StringValue(user.ID)
	state.UserName = types.StringValue(user.UserName)
	state.Email = types.StringValue(email)
	// The Langfuse SCIM API never includes the "active" field in its responses.
	// Overwriting state.Active from the zero-valued response would cause perpetual
	// drift (state=false, config default=true). Preserve whatever is already in state.

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *userResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan userResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state userResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	organizationClient := r.ClientFactory.NewOrganizationClient(
		state.OrganizationPublicKey.ValueString(),
		state.OrganizationPrivateKey.ValueString(),
	)

	email := plan.Email.ValueString()
	active := true
	if !plan.Active.IsNull() && !plan.Active.IsUnknown() {
		active = plan.Active.ValueBool()
	}

	scimReq := &langfuse.SCIMUserRequest{
		Schemas:  []string{"urn:ietf:params:scim:schemas:core:2.0:User"},
		UserName: email,
		Active:   active,
		Emails: []struct {
			Value   string `json:"value"`
			Primary bool   `json:"primary"`
		}{
			{Value: email, Primary: true},
		},
	}

	user, err := organizationClient.UpdateSCIMUser(ctx, state.ID.ValueString(), scimReq)
	if err != nil {
		resp.Diagnostics.AddError("Error updating user", err.Error())
		return
	}

	plan.ID = state.ID
	plan.UserName = types.StringValue(user.UserName)
	// Same rationale as Create: Langfuse's SCIM PUT response omits "active",
	// so we keep the planned value; Read will sync the real state.

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *userResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state userResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !state.IgnoreDestroy.IsNull() && state.IgnoreDestroy.ValueBool() {
		return
	}

	organizationClient := r.ClientFactory.NewOrganizationClient(
		state.OrganizationPublicKey.ValueString(),
		state.OrganizationPrivateKey.ValueString(),
	)

	err := organizationClient.DeleteSCIMUser(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error deleting user", err.Error())
		return
	}
}

func (r *userResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
