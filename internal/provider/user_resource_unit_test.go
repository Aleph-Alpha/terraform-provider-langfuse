package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/langfuse/terraform-provider-langfuse/internal/langfuse"
	"github.com/langfuse/terraform-provider-langfuse/internal/langfuse/mocks"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	resschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestUserResourceMetadata(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	r := NewUserResource().(*userResource)

	var resp resource.MetadataResponse
	r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "langfuse"}, &resp)

	expected := "langfuse_user"
	if resp.TypeName != expected {
		t.Fatalf("unexpected type name. got %q, want %q", resp.TypeName, expected)
	}
}

func TestUserResourceSchema(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	r := NewUserResource().(*userResource)

	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)

	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics from Schema: %v", schemaResp.Diagnostics)
	}
	if diags := schemaResp.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatalf("schema implementation validation failed: %v", diags)
	}

	schema := schemaResp.Schema

	expectedAttributes := []string{
		"id", "email", "user_name", "active",
		"organization_public_key", "organization_private_key", "ignore_destroy",
	}
	for _, expectedAttr := range expectedAttributes {
		if _, exists := schema.Attributes[expectedAttr]; !exists {
			t.Errorf("expected attribute %q not found in schema", expectedAttr)
		}
	}

	idAttr, ok := schema.Attributes["id"].(resschema.StringAttribute)
	if !ok || !idAttr.Computed {
		t.Fatalf("'id' must be a computed string attribute")
	}

	emailAttr, ok := schema.Attributes["email"].(resschema.StringAttribute)
	if !ok || !emailAttr.Required {
		t.Fatalf("'email' must be a required string attribute")
	}

	activeAttr, ok := schema.Attributes["active"].(resschema.BoolAttribute)
	if !ok || !activeAttr.Optional || !activeAttr.Computed {
		t.Fatalf("'active' must be an optional+computed bool attribute")
	}

	orgPubAttr, ok := schema.Attributes["organization_public_key"].(resschema.StringAttribute)
	if !ok || !orgPubAttr.Required || !orgPubAttr.Sensitive {
		t.Fatalf("'organization_public_key' must be a required sensitive string attribute")
	}

	orgPrivAttr, ok := schema.Attributes["organization_private_key"].(resschema.StringAttribute)
	if !ok || !orgPrivAttr.Required || !orgPrivAttr.Sensitive {
		t.Fatalf("'organization_private_key' must be a required sensitive string attribute")
	}
}

func TestUserResourceCRUD(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()

	r, ok := NewUserResource().(*userResource)
	if !ok {
		t.Fatalf("factory did not return *userResource")
	}

	clientFactory := mocks.NewMockClientFactory(ctrl)

	var resourceSchema resschema.Schema
	t.Run("Configure", func(t *testing.T) {
		var configureResp resource.ConfigureResponse
		r.Configure(ctx, resource.ConfigureRequest{ProviderData: clientFactory}, &configureResp)
		if configureResp.Diagnostics.HasError() {
			t.Fatalf("unexpected diagnostics from Configure: %v", configureResp.Diagnostics)
		}
		if r.ClientFactory == nil {
			t.Fatalf("ClientFactory is nil after Configure")
		}
		var schemaResp resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
		if schemaResp.Diagnostics.HasError() {
			t.Fatalf("unexpected diagnostics from Schema: %v", schemaResp.Diagnostics)
		}
		resourceSchema = schemaResp.Schema
	})

	userID := "user-scim-123"
	email := "user@example.com"

	var createResp resource.CreateResponse
	t.Run("Create", func(t *testing.T) {
		clientFactory.OrganizationClient.EXPECT().
			CreateSCIMUser(ctx, gomock.Any()).
			Return(&langfuse.SCIMUserResponse{
				ID:       userID,
				UserName: email,
				Active:   true,
				Emails: []struct {
					Value   string `json:"value"`
					Primary bool   `json:"primary"`
				}{
					{Value: email, Primary: true},
				},
			}, nil)

		createResp.State.Schema = resourceSchema
		r.Create(ctx, resource.CreateRequest{
			Plan: tfsdk.Plan{
				Schema: resourceSchema,
				Raw: buildUserObjectValue(map[string]tftypes.Value{
					"id":                       tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
					"email":                    tftypes.NewValue(tftypes.String, email),
					"user_name":                tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
					"active":                   tftypes.NewValue(tftypes.Bool, true),
					"organization_public_key":  tftypes.NewValue(tftypes.String, "pub-key"),
					"organization_private_key": tftypes.NewValue(tftypes.String, "priv-key"),
					"ignore_destroy":           tftypes.NewValue(tftypes.Bool, nil),
				}),
			},
		}, &createResp)
		if createResp.Diagnostics.HasError() {
			t.Fatalf("unexpected diagnostics from Create: %v", createResp.Diagnostics)
		}
	})

	var readResp resource.ReadResponse
	t.Run("Read", func(t *testing.T) {
		clientFactory.OrganizationClient.EXPECT().
			FindSCIMUserByEmail(ctx, email).
			Return(&langfuse.SCIMUserResponse{
				ID:       userID,
				UserName: email,
				Active:   true,
				Emails: []struct {
					Value   string `json:"value"`
					Primary bool   `json:"primary"`
				}{
					{Value: email, Primary: true},
				},
			}, nil)

		readResp.State.Schema = resourceSchema
		r.Read(ctx, resource.ReadRequest{State: createResp.State}, &readResp)
		if readResp.Diagnostics.HasError() {
			t.Fatalf("unexpected diagnostics from Read: %v", readResp.Diagnostics)
		}
	})

	var updateResp resource.UpdateResponse
	t.Run("Update", func(t *testing.T) {
		clientFactory.OrganizationClient.EXPECT().
			UpdateSCIMUser(ctx, userID, gomock.Any()).
			Return(&langfuse.SCIMUserResponse{
				ID:       userID,
				UserName: email,
				Active:   false,
				Emails: []struct {
					Value   string `json:"value"`
					Primary bool   `json:"primary"`
				}{
					{Value: email, Primary: true},
				},
			}, nil)

		updateResp.State.Schema = resourceSchema
		r.Update(ctx, resource.UpdateRequest{
			Plan: tfsdk.Plan{
				Schema: resourceSchema,
				Raw: buildUserObjectValue(map[string]tftypes.Value{
					"id":                       tftypes.NewValue(tftypes.String, userID),
					"email":                    tftypes.NewValue(tftypes.String, email),
					"user_name":                tftypes.NewValue(tftypes.String, email),
					"active":                   tftypes.NewValue(tftypes.Bool, false),
					"organization_public_key":  tftypes.NewValue(tftypes.String, "pub-key"),
					"organization_private_key": tftypes.NewValue(tftypes.String, "priv-key"),
					"ignore_destroy":           tftypes.NewValue(tftypes.Bool, nil),
				}),
			},
			State: readResp.State,
		}, &updateResp)
		if updateResp.Diagnostics.HasError() {
			t.Fatalf("unexpected diagnostics from Update: %v", updateResp.Diagnostics)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		clientFactory.OrganizationClient.EXPECT().
			DeleteSCIMUser(ctx, userID).
			Return(nil)

		var deleteResp resource.DeleteResponse
		deleteResp.State.Schema = resourceSchema
		r.Delete(ctx, resource.DeleteRequest{State: updateResp.State}, &deleteResp)
		if deleteResp.Diagnostics.HasError() {
			t.Fatalf("unexpected diagnostics from Delete: %v", deleteResp.Diagnostics)
		}
	})
}

func TestUserResource_Read_NotFound(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()

	clientFactory := mocks.NewMockClientFactory(ctrl)
	r := &userResource{}
	var configureResp resource.ConfigureResponse
	r.Configure(ctx, resource.ConfigureRequest{ProviderData: clientFactory}, &configureResp)

	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)

	state := tfsdk.State{
		Schema: schemaResp.Schema,
		Raw: buildUserObjectValue(map[string]tftypes.Value{
			"id":                       tftypes.NewValue(tftypes.String, "user-scim-123"),
			"email":                    tftypes.NewValue(tftypes.String, "user@example.com"),
			"user_name":                tftypes.NewValue(tftypes.String, "user@example.com"),
			"active":                   tftypes.NewValue(tftypes.Bool, true),
			"organization_public_key":  tftypes.NewValue(tftypes.String, "pub-key"),
			"organization_private_key": tftypes.NewValue(tftypes.String, "priv-key"),
			"ignore_destroy":           tftypes.NewValue(tftypes.Bool, nil),
		}),
	}

	clientFactory.OrganizationClient.EXPECT().
		FindSCIMUserByEmail(ctx, "user@example.com").
		Return(nil, fmt.Errorf("cannot find user with email %q", "user@example.com"))

	var resp resource.ReadResponse
	resp.State.Schema = schemaResp.Schema
	r.Read(ctx, resource.ReadRequest{State: state}, &resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("expected no diagnostics, got: %v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Fatal("expected state to be removed (null) when user is not found")
	}
}

func TestUserResource_Read_Error(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()

	clientFactory := mocks.NewMockClientFactory(ctrl)
	r := &userResource{}
	var configureResp resource.ConfigureResponse
	r.Configure(ctx, resource.ConfigureRequest{ProviderData: clientFactory}, &configureResp)

	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)

	state := tfsdk.State{
		Schema: schemaResp.Schema,
		Raw: buildUserObjectValue(map[string]tftypes.Value{
			"id":                       tftypes.NewValue(tftypes.String, "user-scim-123"),
			"email":                    tftypes.NewValue(tftypes.String, "user@example.com"),
			"user_name":                tftypes.NewValue(tftypes.String, "user@example.com"),
			"active":                   tftypes.NewValue(tftypes.Bool, true),
			"organization_public_key":  tftypes.NewValue(tftypes.String, "pub-key"),
			"organization_private_key": tftypes.NewValue(tftypes.String, "priv-key"),
			"ignore_destroy":           tftypes.NewValue(tftypes.Bool, nil),
		}),
	}

	clientFactory.OrganizationClient.EXPECT().
		FindSCIMUserByEmail(ctx, "user@example.com").
		Return(nil, fmt.Errorf("internal server error"))

	var resp resource.ReadResponse
	resp.State.Schema = schemaResp.Schema
	r.Read(ctx, resource.ReadRequest{State: state}, &resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected error diagnostic for non-404 error, got none")
	}
	errs := resp.Diagnostics.Errors()
	if errs[0].Summary() != "Error reading user" {
		t.Fatalf("unexpected error summary: %q", errs[0].Summary())
	}
}

func TestUserResource_Delete_IgnoreDestroy(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()

	// No calls expected on the client — ignore_destroy=true skips deletion.
	clientFactory := mocks.NewMockClientFactory(ctrl)
	r := &userResource{}
	var configureResp resource.ConfigureResponse
	r.Configure(ctx, resource.ConfigureRequest{ProviderData: clientFactory}, &configureResp)

	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)

	state := tfsdk.State{
		Schema: schemaResp.Schema,
		Raw: buildUserObjectValue(map[string]tftypes.Value{
			"id":                       tftypes.NewValue(tftypes.String, "user-scim-123"),
			"email":                    tftypes.NewValue(tftypes.String, "user@example.com"),
			"user_name":                tftypes.NewValue(tftypes.String, "user@example.com"),
			"active":                   tftypes.NewValue(tftypes.Bool, true),
			"organization_public_key":  tftypes.NewValue(tftypes.String, "pub-key"),
			"organization_private_key": tftypes.NewValue(tftypes.String, "priv-key"),
			"ignore_destroy":           tftypes.NewValue(tftypes.Bool, true),
		}),
	}

	var deleteResp resource.DeleteResponse
	deleteResp.State.Schema = schemaResp.Schema
	r.Delete(ctx, resource.DeleteRequest{State: state}, &deleteResp)

	if deleteResp.Diagnostics.HasError() {
		t.Fatalf("expected no diagnostics when ignore_destroy=true, got: %v", deleteResp.Diagnostics)
	}
}

func buildUserObjectValue(values map[string]tftypes.Value) tftypes.Value {
	return tftypes.NewValue(
		tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{
				"id":                       tftypes.String,
				"email":                    tftypes.String,
				"user_name":                tftypes.String,
				"active":                   tftypes.Bool,
				"organization_public_key":  tftypes.String,
				"organization_private_key": tftypes.String,
				"ignore_destroy":           tftypes.Bool,
			},
			OptionalAttributes: map[string]struct{}{
				"id":             {},
				"user_name":      {},
				"active":         {},
				"ignore_destroy": {},
			},
		},
		values,
	)
}
