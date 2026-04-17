package provider

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/langfuse/terraform-provider-langfuse/internal/langfuse"
)

// TestAccLangfuseUserWorkflow tests the complete lifecycle of the langfuse_user
// resource and verifies that the langfuse_user data source can look up users
// by email and by ID.
func TestAccLangfuseUserWorkflow(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC not set - skipping acceptance test")
	}
	testAccPreCheck(t)

	orgName := fmt.Sprintf("user-wf-org-%d", rand.Intn(1000000))
	userEmail := fmt.Sprintf("user-wf-%d@example.com", rand.Intn(1000000))

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckLangfuseResourcesDestroyed,
		Steps: []resource.TestStep{
			// Step 1: Create org, org API key, and user.
			{
				Config: testAccUserConfig_Create(orgName, userEmail),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("langfuse_user.test", "id"),
					resource.TestCheckResourceAttr("langfuse_user.test", "email", userEmail),
					resource.TestCheckResourceAttr("langfuse_user.test", "active", "true"),
					resource.TestCheckResourceAttrSet("langfuse_user.test", "user_name"),
				),
			},
			// Step 2: Look up the same user by email via the data source.
			{
				Config: testAccUserConfig_DataSourceByEmail(orgName, userEmail),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("langfuse_user.test", "email", userEmail),
					resource.TestCheckResourceAttrSet("data.langfuse_user.by_email", "id"),
					resource.TestCheckResourceAttr("data.langfuse_user.by_email", "email", userEmail),
					resource.TestCheckResourceAttr("data.langfuse_user.by_email", "active", "true"),
					resource.TestCheckResourceAttrSet("data.langfuse_user.by_email", "user_name"),
				),
			},
			// Step 3: Look up the same user by ID via the data source.
			{
				Config: testAccUserConfig_DataSourceByID(orgName, userEmail),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.langfuse_user.by_id", "id"),
					resource.TestCheckResourceAttr("data.langfuse_user.by_id", "email", userEmail),
					resource.TestCheckResourceAttr("data.langfuse_user.by_id", "active", "true"),
				),
			},
			// Step 4: Cleanup — remove all resources.
			// Note: setting active=false via SCIM causes Langfuse to fully remove the user,
			// so a deactivation step is omitted here.
			{
				Config: testAccUserMembershipProviderOnly(),
			},
		},
	})
}

// TestAccLangfuseProjectMembershipWorkflow tests the complete lifecycle of the
// langfuse_project_membership resource, including a role change.
func TestAccLangfuseProjectMembershipWorkflow(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC not set - skipping acceptance test")
	}
	testAccPreCheck(t)

	orgName := fmt.Sprintf("membership-wf-org-%d", rand.Intn(1000000))
	projectName := fmt.Sprintf("membership-wf-proj-%d", rand.Intn(1000000))
	userEmail := fmt.Sprintf("membership-wf-%d@example.com", rand.Intn(1000000))

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckLangfuseResourcesDestroyed,
		Steps: []resource.TestStep{
			// Step 1: Create org, org API key, project, user, and project membership with MEMBER role.
			{
				Config: testAccProjectMembershipConfig_Create(orgName, projectName, userEmail, "MEMBER"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("langfuse_project_membership.test", "id"),
					resource.TestCheckResourceAttr("langfuse_project_membership.test", "role", "MEMBER"),
					resource.TestCheckResourceAttrSet("langfuse_project_membership.test", "email"),
					resource.TestCheckResourceAttr("langfuse_user.test", "email", userEmail),
				),
			},
			// Step 2: Update the membership role to VIEWER.
			{
				Config: testAccProjectMembershipConfig_Create(orgName, projectName, userEmail, "VIEWER"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("langfuse_project_membership.test", "role", "VIEWER"),
					resource.TestCheckResourceAttrSet("langfuse_project_membership.test", "email"),
				),
			},
			// Step 3: Cleanup — remove all resources.
			{
				Config: testAccUserMembershipProviderOnly(),
			},
		},
	})
}

// TestAccLangfuseProjectApiKeyDeleteAndRecreate verifies that when a project API key
// is deleted directly in Langfuse (outside of Terraform, e.g. via the UI), the
// provider detects the drift on the next plan/apply and creates a fresh key.
func TestAccLangfuseProjectApiKeyDeleteAndRecreate(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC not set - skipping acceptance test")
	}
	testAccPreCheck(t)

	orgName := fmt.Sprintf("apikey-recreate-org-%d", rand.Intn(1000000))
	projectName := fmt.Sprintf("apikey-recreate-proj-%d", rand.Intn(1000000))

	// These are set in step 1's Check and consumed in step 2's PreConfig.
	var (
		capturedPublicKey  string
		capturedPrivateKey string
		capturedProjectID  string
		capturedKeyID      string
	)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckLangfuseResourcesDestroyed,
		Steps: []resource.TestStep{
			// Step 1: Create the stack and capture the API key credentials for later.
			{
				Config: testAccProjectApiKeyRecreateConfig(orgName, projectName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("langfuse_project_api_key.test", "id"),
					resource.TestCheckResourceAttrSet("langfuse_project_api_key.test", "public_key"),
					resource.TestCheckResourceAttrSet("langfuse_project_api_key.test", "secret_key"),
					func(s *terraform.State) error {
						rs, ok := s.RootModule().Resources["langfuse_project_api_key.test"]
						if !ok {
							return fmt.Errorf("langfuse_project_api_key.test not found in state")
						}
						capturedPublicKey = rs.Primary.Attributes["organization_public_key"]
						capturedPrivateKey = rs.Primary.Attributes["organization_private_key"]
						capturedProjectID = rs.Primary.Attributes["project_id"]
						capturedKeyID = rs.Primary.ID
						return nil
					},
				),
			},
			// Step 2: Delete the project API key directly in Langfuse (simulating a manual
			// deletion). PreConfig runs before the refresh/plan/apply cycle, so the provider's
			// Read will detect the key is gone, remove it from state, and the plan will show
			// a create action. The apply then produces a brand-new key.
			{
				PreConfig: func() {
					host := os.Getenv("LANGFUSE_HOST")
					client := langfuse.NewOrganizationClient(host, capturedPublicKey, capturedPrivateKey)
					if err := client.DeleteProjectApiKey(context.Background(), capturedProjectID, capturedKeyID); err != nil {
						t.Fatalf("failed to delete project API key externally: %v", err)
					}
				},
				Config: testAccProjectApiKeyRecreateConfig(orgName, projectName),
				Check: resource.ComposeAggregateTestCheckFunc(
					// A new key was created after drift detection — verify it exists.
					resource.TestCheckResourceAttrSet("langfuse_project_api_key.test", "id"),
					resource.TestCheckResourceAttrSet("langfuse_project_api_key.test", "public_key"),
					resource.TestCheckResourceAttrSet("langfuse_project_api_key.test", "secret_key"),
				),
			},
			// Cleanup.
			{
				Config: testAccUserMembershipProviderOnly(),
			},
		},
	})
}

// TestAccLangfuseIgnoreDestroy verifies that langfuse_user, langfuse_project_membership,
// and langfuse_project_api_key resources configured with ignore_destroy = true are NOT
// deleted from Langfuse when Terraform removes them from state.
func TestAccLangfuseIgnoreDestroy(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC not set - skipping acceptance test")
	}
	testAccPreCheck(t)

	orgName := fmt.Sprintf("ignore-destroy-org-%d", rand.Intn(1000000))
	projectName := fmt.Sprintf("ignore-destroy-proj-%d", rand.Intn(1000000))
	userEmail := fmt.Sprintf("ignore-destroy-%d@example.com", rand.Intn(1000000))

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckLangfuseResourcesDestroyed,
		Steps: []resource.TestStep{
			// Step 1: Create user, project_membership, and project_api_key, all with ignore_destroy=true.
			{
				Config: testAccIgnoreDestroyConfig_Create(orgName, projectName, userEmail),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("langfuse_user.test", "id"),
					resource.TestCheckResourceAttr("langfuse_user.test", "email", userEmail),
					resource.TestCheckResourceAttr("langfuse_user.test", "ignore_destroy", "true"),
					resource.TestCheckResourceAttrSet("langfuse_project_membership.test", "id"),
					resource.TestCheckResourceAttr("langfuse_project_membership.test", "role", "MEMBER"),
					resource.TestCheckResourceAttr("langfuse_project_membership.test", "ignore_destroy", "true"),
					resource.TestCheckResourceAttrSet("langfuse_project_api_key.test", "id"),
					resource.TestCheckResourceAttr("langfuse_project_api_key.test", "ignore_destroy", "true"),
				),
			},
			// Step 2: Remove user, project_membership, and project_api_key from the config.
			// Terraform calls Delete on each, but ignore_destroy=true causes the provider to
			// skip the actual Langfuse API call — the objects remain in Langfuse.
			{
				Config: testAccIgnoreDestroyConfig_RemoveIgnored(orgName, projectName),
			},
			// Step 3: Confirm the user still exists in Langfuse by reading it via the data source.
			// If ignore_destroy worked correctly, the API call succeeds and the data is returned.
			{
				Config: testAccIgnoreDestroyConfig_VerifyUser(orgName, projectName, userEmail),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.langfuse_user.verify", "id"),
					resource.TestCheckResourceAttr("data.langfuse_user.verify", "email", userEmail),
					resource.TestCheckResourceAttr("data.langfuse_user.verify", "active", "true"),
				),
			},
			// Cleanup — remove remaining resources (org, org API key, project).
			{
				Config: testAccUserMembershipProviderOnly(),
			},
		},
	})
}

// ── Shared helpers ────────────────────────────────────────────────────────────

// testAccUserMembershipProviderOnly returns a config containing only the provider
// block. Applied as a cleanup step it removes all resources in dependency order.
func testAccUserMembershipProviderOnly() string {
	return fmt.Sprintf(`
provider "langfuse" {
  host          = "%s"
  admin_api_key = "%s"
}
`, os.Getenv("LANGFUSE_HOST"), os.Getenv("LANGFUSE_ADMIN_KEY"))
}

// ── User workflow config helpers ──────────────────────────────────────────────

func testAccUserConfig_Create(orgName, userEmail string) string {
	return fmt.Sprintf(`
provider "langfuse" {
  host          = "%s"
  admin_api_key = "%s"
}

resource "langfuse_organization" "user_test" {
  name = "%s"
}

resource "langfuse_organization_api_key" "user_test" {
  organization_id = langfuse_organization.user_test.id
}

resource "langfuse_user" "test" {
  email                    = "%s"
  organization_public_key  = langfuse_organization_api_key.user_test.public_key
  organization_private_key = langfuse_organization_api_key.user_test.secret_key
}
`, os.Getenv("LANGFUSE_HOST"), os.Getenv("LANGFUSE_ADMIN_KEY"), orgName, userEmail)
}

func testAccUserConfig_DataSourceByEmail(orgName, userEmail string) string {
	return fmt.Sprintf(`
provider "langfuse" {
  host          = "%s"
  admin_api_key = "%s"
}

resource "langfuse_organization" "user_test" {
  name = "%s"
}

resource "langfuse_organization_api_key" "user_test" {
  organization_id = langfuse_organization.user_test.id
}

resource "langfuse_user" "test" {
  email                    = "%s"
  organization_public_key  = langfuse_organization_api_key.user_test.public_key
  organization_private_key = langfuse_organization_api_key.user_test.secret_key
}

data "langfuse_user" "by_email" {
  email                    = "%s"
  organization_public_key  = langfuse_organization_api_key.user_test.public_key
  organization_private_key = langfuse_organization_api_key.user_test.secret_key

  depends_on = [langfuse_user.test]
}
`, os.Getenv("LANGFUSE_HOST"), os.Getenv("LANGFUSE_ADMIN_KEY"), orgName, userEmail, userEmail)
}

func testAccUserConfig_DataSourceByID(orgName, userEmail string) string {
	return fmt.Sprintf(`
provider "langfuse" {
  host          = "%s"
  admin_api_key = "%s"
}

resource "langfuse_organization" "user_test" {
  name = "%s"
}

resource "langfuse_organization_api_key" "user_test" {
  organization_id = langfuse_organization.user_test.id
}

resource "langfuse_user" "test" {
  email                    = "%s"
  organization_public_key  = langfuse_organization_api_key.user_test.public_key
  organization_private_key = langfuse_organization_api_key.user_test.secret_key
}

data "langfuse_user" "by_id" {
  id                       = langfuse_user.test.id
  organization_public_key  = langfuse_organization_api_key.user_test.public_key
  organization_private_key = langfuse_organization_api_key.user_test.secret_key
}
`, os.Getenv("LANGFUSE_HOST"), os.Getenv("LANGFUSE_ADMIN_KEY"), orgName, userEmail)
}


// ── Project membership config helpers ─────────────────────────────────────────

func testAccProjectMembershipConfig_Create(orgName, projectName, userEmail, role string) string {
	return fmt.Sprintf(`
provider "langfuse" {
  host          = "%s"
  admin_api_key = "%s"
}

resource "langfuse_organization" "membership_test" {
  name = "%s"
}

resource "langfuse_organization_api_key" "membership_test" {
  organization_id = langfuse_organization.membership_test.id
}

resource "langfuse_project" "membership_test" {
  name                     = "%s"
  retention_days           = 0
  organization_id          = langfuse_organization.membership_test.id
  organization_public_key  = langfuse_organization_api_key.membership_test.public_key
  organization_private_key = langfuse_organization_api_key.membership_test.secret_key
}

resource "langfuse_user" "test" {
  email                    = "%s"
  organization_public_key  = langfuse_organization_api_key.membership_test.public_key
  organization_private_key = langfuse_organization_api_key.membership_test.secret_key
}

resource "langfuse_project_membership" "test" {
  project_id               = langfuse_project.membership_test.id
  user_id                  = langfuse_user.test.id
  role                     = "%s"
  organization_public_key  = langfuse_organization_api_key.membership_test.public_key
  organization_private_key = langfuse_organization_api_key.membership_test.secret_key
}
`, os.Getenv("LANGFUSE_HOST"), os.Getenv("LANGFUSE_ADMIN_KEY"), orgName, projectName, userEmail, role)
}

// ── Project API key delete+recreate config helpers ────────────────────────────

func testAccProjectApiKeyRecreateConfig(orgName, projectName string) string {
	return fmt.Sprintf(`
provider "langfuse" {
  host          = "%s"
  admin_api_key = "%s"
}

resource "langfuse_organization" "apikey_recreate" {
  name = "%s"
}

resource "langfuse_organization_api_key" "apikey_recreate" {
  organization_id = langfuse_organization.apikey_recreate.id
}

resource "langfuse_project" "apikey_recreate" {
  name                     = "%s"
  retention_days           = 0
  organization_id          = langfuse_organization.apikey_recreate.id
  organization_public_key  = langfuse_organization_api_key.apikey_recreate.public_key
  organization_private_key = langfuse_organization_api_key.apikey_recreate.secret_key
}

resource "langfuse_project_api_key" "test" {
  project_id               = langfuse_project.apikey_recreate.id
  organization_public_key  = langfuse_organization_api_key.apikey_recreate.public_key
  organization_private_key = langfuse_organization_api_key.apikey_recreate.secret_key
  note                     = "delete-and-recreate-test"
}
`, os.Getenv("LANGFUSE_HOST"), os.Getenv("LANGFUSE_ADMIN_KEY"), orgName, projectName)
}

// ── Ignore-destroy config helpers ─────────────────────────────────────────────

func testAccIgnoreDestroyConfig_Create(orgName, projectName, userEmail string) string {
	return fmt.Sprintf(`
provider "langfuse" {
  host          = "%s"
  admin_api_key = "%s"
}

resource "langfuse_organization" "ignore_destroy" {
  name = "%s"
}

resource "langfuse_organization_api_key" "ignore_destroy" {
  organization_id = langfuse_organization.ignore_destroy.id
}

resource "langfuse_project" "ignore_destroy" {
  name                     = "%s"
  retention_days           = 0
  organization_id          = langfuse_organization.ignore_destroy.id
  organization_public_key  = langfuse_organization_api_key.ignore_destroy.public_key
  organization_private_key = langfuse_organization_api_key.ignore_destroy.secret_key
}

resource "langfuse_user" "test" {
  email                    = "%s"
  organization_public_key  = langfuse_organization_api_key.ignore_destroy.public_key
  organization_private_key = langfuse_organization_api_key.ignore_destroy.secret_key
  ignore_destroy           = true
}

resource "langfuse_project_membership" "test" {
  project_id               = langfuse_project.ignore_destroy.id
  user_id                  = langfuse_user.test.id
  role                     = "MEMBER"
  organization_public_key  = langfuse_organization_api_key.ignore_destroy.public_key
  organization_private_key = langfuse_organization_api_key.ignore_destroy.secret_key
  ignore_destroy           = true
}

resource "langfuse_project_api_key" "test" {
  project_id               = langfuse_project.ignore_destroy.id
  organization_public_key  = langfuse_organization_api_key.ignore_destroy.public_key
  organization_private_key = langfuse_organization_api_key.ignore_destroy.secret_key
  ignore_destroy           = true
}
`, os.Getenv("LANGFUSE_HOST"), os.Getenv("LANGFUSE_ADMIN_KEY"), orgName, projectName, userEmail)
}

// testAccIgnoreDestroyConfig_RemoveIgnored keeps the org/org-api-key/project but
// drops the user, project_membership, and project_api_key resources from the config,
// triggering Delete calls that should be no-ops due to ignore_destroy=true.
func testAccIgnoreDestroyConfig_RemoveIgnored(orgName, projectName string) string {
	return fmt.Sprintf(`
provider "langfuse" {
  host          = "%s"
  admin_api_key = "%s"
}

resource "langfuse_organization" "ignore_destroy" {
  name = "%s"
}

resource "langfuse_organization_api_key" "ignore_destroy" {
  organization_id = langfuse_organization.ignore_destroy.id
}

resource "langfuse_project" "ignore_destroy" {
  name                     = "%s"
  retention_days           = 0
  organization_id          = langfuse_organization.ignore_destroy.id
  organization_public_key  = langfuse_organization_api_key.ignore_destroy.public_key
  organization_private_key = langfuse_organization_api_key.ignore_destroy.secret_key
}
`, os.Getenv("LANGFUSE_HOST"), os.Getenv("LANGFUSE_ADMIN_KEY"), orgName, projectName)
}

// testAccIgnoreDestroyConfig_VerifyUser extends the "remove ignored" config with a
// data source that reads the user by email. A successful read confirms the user was
// not deleted despite Terraform having "destroyed" the resource.
func testAccIgnoreDestroyConfig_VerifyUser(orgName, projectName, userEmail string) string {
	return testAccIgnoreDestroyConfig_RemoveIgnored(orgName, projectName) + fmt.Sprintf(`
data "langfuse_user" "verify" {
  email                    = "%s"
  organization_public_key  = langfuse_organization_api_key.ignore_destroy.public_key
  organization_private_key = langfuse_organization_api_key.ignore_destroy.secret_key
}
`, userEmail)
}
