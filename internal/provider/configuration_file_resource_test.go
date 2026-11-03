package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

func testAccClient(t *testing.T) *client.Client {
	t.Helper()
	c, err := client.New(context.Background(), client.Config{URL: os.Getenv("TENZIR_PLATFORM_URL"), APIKey: os.Getenv("TENZIR_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// draftScope is the test user's draft on main of the workspace with the
// given slug.
func draftScope(t *testing.T, c *client.Client, workspace string) client.DraftScope {
	t.Helper()
	account, err := c.ReadAccount(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return client.DraftScope{Organization: os.Getenv("TENZIR_TEST_ORGANIZATION"), Workspace: workspace, UserID: account.User.ID, Branch: "main"}
}

func TestAccConfigurationFile(t *testing.T) {
	workspace := testAccName("tf-acc-config")
	const file = "tenzir_configuration_file.test"
	config := func(content, message string, withFile bool) string {
		hcl := testAccOrganization() + fmt.Sprintf(`
resource "tenzir_workspace" "test" {
  organization_id = data.tenzir_organization.test.id
  slug            = %q
}

resource "tenzir_deployment" "test" {
  organization_id = data.tenzir_organization.test.id
  workspace_id    = tenzir_workspace.test.id
  name            = "follows main"
}
`, workspace)
		if withFile {
			hcl += fmt.Sprintf(`
resource "tenzir_configuration_file" "test" {
  organization_id = data.tenzir_organization.test.id
  workspace_id    = tenzir_workspace.test.id
  path            = "pipelines/ingest.tql"
  content         = %q
  commit_message  = %s
}
`, content, message)
		}
		return hcl
	}
	var c *client.Client
	var foreign *client.Draft
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			c = testAccClient(t)
		},
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: config("from {x: 1}\n", "null", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(file, "branch", "main"),
					checkCommit(&c, file, "terraform: create pipelines/ingest.tql"),
					checkDeploymentSeesCommit(&c, file),
					checkNoDraft(&c, workspace),
				),
			},
			{
				ResourceName:            file,
				ImportState:             true,
				ImportStateIdFunc:       configurationFileImportID(file),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"commit_message"},
			},
			{
				Config: config("from {x: 2}\n", `"Count to two"`, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(file, plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					checkCommit(&c, file, "Count to two"),
					checkDeploymentSeesCommit(&c, file),
				),
			},
			{
				PreConfig: func() {
					s := draftScope(t, c, workspace)
					d, err := c.CreateDraft(context.Background(), s)
					if err != nil {
						t.Fatal(err)
					}
					text := "from {x: 0}\n"
					foreign, err = c.SaveDraft(context.Background(), s, d.HeadSHA, nil,
						[]client.FileChange{{Type: "put", Path: "pipelines/ingest.tql", Text: &text}})
					if err != nil {
						t.Fatal(err)
					}
				},
				Config:      config("from {x: 3}\n", `"Count to three"`, true),
				ExpectError: regexp.MustCompile(`unpublished changes in a draft`),
			},
			{
				PreConfig: func() {
					s := draftScope(t, c, workspace)
					commit, err := c.CommitDraft(context.Background(), s)
					if err != nil || commit != foreign.HeadSHA {
						t.Fatalf("committing the draft from elsewhere: %v", err)
					}
				},
				Config: config("from {x: 3}\n", `"Count to three"`, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(file, "content", "from {x: 3}\n"),
					checkCommit(&c, file, "Count to three"),
					checkDeploymentSeesCommit(&c, file),
				),
			},
			{
				Config: config("", "null", false),
				Check: func(*terraform.State) error {
					tree, err := c.ReadFileTree(context.Background(), os.Getenv("TENZIR_TEST_ORGANIZATION"), workspace, "main")
					if err != nil {
						return err
					}
					for _, e := range tree.Entries {
						if e.Path == "pipelines/ingest.tql" {
							return fmt.Errorf("the file outlived its resource")
						}
					}
					details, err := c.ReadCommit(context.Background(), os.Getenv("TENZIR_TEST_ORGANIZATION"), workspace, tree.Commit)
					if err != nil {
						return err
					}
					if details.Message != "terraform: delete pipelines/ingest.tql" {
						return fmt.Errorf("the delete commit says %q", details.Message)
					}
					return nil
				},
			},
		},
	})
}

// TestAccConfigurationFiles writes several files to one branch at once,
// which Terraform does in parallel.
func TestAccConfigurationFiles(t *testing.T) {
	workspace := testAccName("tf-acc-configs")
	config := func(version int) string {
		return testAccOrganization() + fmt.Sprintf(`
resource "tenzir_workspace" "test" {
  organization_id = data.tenzir_organization.test.id
  slug            = %q
}

resource "tenzir_configuration_file" "test" {
  count           = 6
  organization_id = data.tenzir_organization.test.id
  workspace_id    = tenzir_workspace.test.slug
  path            = "pipelines/${count.index}.tql"
  content         = "from {pipeline: ${count.index}, version: %d}\n"
}
`, workspace, version)
	}
	var c *client.Client
	checkAll := func(verb string) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			org := os.Getenv("TENZIR_TEST_ORGANIZATION")
			tree, err := c.ReadFileTree(context.Background(), org, workspace, "main")
			if err != nil {
				return err
			}
			files := 0
			for _, e := range tree.Entries {
				if e.Kind == "file" {
					files++
				}
			}
			if files != 6 {
				return fmt.Errorf("main holds %d files, not 6", files)
			}
			commits := map[string]bool{}
			for name, rs := range s.RootModule().Resources {
				if rs.Type != "tenzir_configuration_file" {
					continue
				}
				commit := rs.Primary.Attributes["commit"]
				commits[commit] = true
				details, err := c.ReadCommit(context.Background(), org, workspace, commit)
				if err != nil {
					return err
				}
				if want := "terraform: " + verb + " " + rs.Primary.Attributes["path"]; details.Message != want {
					return fmt.Errorf("%s: commit %s says %q, not %q", name, commit, details.Message, want)
				}
			}
			if len(commits) != 6 {
				return fmt.Errorf("six writes made %d commits", len(commits))
			}
			return checkNoDraft(&c, workspace)(s)
		}
	}
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			c = testAccClient(t)
		},
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{Config: config(1), Check: checkAll("create")},
			{Config: config(2), Check: checkAll("update")},
		},
	})
}

func configurationFileImportID(name string) func(*terraform.State) (string, error) {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return "", fmt.Errorf("%s is not in state", name)
		}
		a := rs.Primary.Attributes
		return a["organization_id"] + "/" + a["workspace_id"] + "/" + a["branch"] + ":" + a["path"], nil
	}
}

// checkCommit checks the message of the commit that the resource recorded.
func checkCommit(c **client.Client, name, message string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		a := s.RootModule().Resources[name].Primary.Attributes
		details, err := (*c).ReadCommit(context.Background(), a["organization_id"], a["workspace_id"], a["commit"])
		if err != nil {
			return err
		}
		if details.Message != message {
			return fmt.Errorf("commit %s says %q, not %q", a["commit"], details.Message, message)
		}
		return nil
	}
}

// checkDeploymentSeesCommit checks that the deployment following the branch
// takes the resource's commit as the newest.
func checkDeploymentSeesCommit(c **client.Client, name string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		commit := s.RootModule().Resources[name].Primary.Attributes["commit"]
		id := s.RootModule().Resources["tenzir_deployment.test"].Primary.ID
		status, err := (*c).ReadDeploymentConfigurationStatus(context.Background(), id)
		if err != nil {
			return err
		}
		if status.Latest == nil || *status.Latest != commit {
			return fmt.Errorf("the deployment sees %v as the newest commit, not %s", status.Latest, commit)
		}
		return nil
	}
}

func checkNoDraft(c **client.Client, workspace string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		account, err := (*c).ReadAccount(context.Background())
		if err != nil {
			return err
		}
		s := client.DraftScope{Organization: os.Getenv("TENZIR_TEST_ORGANIZATION"), Workspace: workspace, UserID: account.User.ID, Branch: "main"}
		_, err = (*c).ReadDraft(context.Background(), s)
		if !client.IsNotFound(err) {
			return fmt.Errorf("a draft is left behind: %v", err)
		}
		return nil
	}
}

func TestConfigurationFileImportID(t *testing.T) {
	org, ws, branch, p, err := splitConfigurationFileID("acme/soc/team/edge:pipelines/a:b.tql")
	if err != nil || org != "acme" || ws != "soc" || branch != "team/edge" || p != "pipelines/a:b.tql" {
		t.Errorf("got %q %q %q %q %v", org, ws, branch, p, err)
	}
	for _, id := range []string{"acme/soc/main", "acme/soc:x", "acme/soc/main:", "acme//main:x"} {
		if _, _, _, _, err := splitConfigurationFileID(id); err == nil {
			t.Errorf("%q: expected an error", id)
		}
	}
}

func TestConfigurationPath(t *testing.T) {
	for p, ok := range map[string]bool{
		"pipelines/ingest.tql":       true,
		"pipelines/team/ingest.tql":  true,
		"tenzir.yaml":                true,
		"packages/zeek/package.yaml": true,
		"pipelines/ingest.txt":       false,
		"packages/README.md":         false,
		"package.yaml":               false,
		"src/x.tql":                  false,
		"/tenzir.yaml":               false,
		"pipelines/../tenzir.yaml":   false,
		".git/config":                false,
		"pipelines\\ingest.tql":      false,
	} {
		if err := checkConfigurationPath(p); (err == nil) != ok {
			t.Errorf("%q: got %v", p, err)
		}
	}
}

// TestPublishFileRetries checks that a commit refused because the branch
// moved discards the draft and succeeds against the new tip.
func TestPublishFileRetries(t *testing.T) {
	var calls []string
	var srv *httptest.Server
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	drafts := "/api/organizations/org-1/workspaces/ws-1/configuration/drafts/user-1/"
	mux.HandleFunc("GET "+client.WellKnownPath, func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"apiUrl": srv.URL + "/api"})
	})
	mux.HandleFunc("GET /api/account", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"_tag": "UserPrincipal", "user": map[string]any{"id": "user-1"}})
	})
	mux.HandleFunc("GET /api/organizations/org-1/workspaces/ws-1/file-tree", func(w http.ResponseWriter, r *http.Request) {
		reply(w, client.GitTree{Commit: fmt.Sprintf("tip-%d", len(calls))})
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		step := strings.TrimPrefix(r.URL.Path, drafts)
		calls = append(calls, r.Method+" "+step)
		switch step {
		case "detail":
			w.WriteHeader(http.StatusNotFound)
			reply(w, map[string]any{"_tag": "NotFound"})
		case "":
			reply(w, client.Draft{HeadSHA: "empty", Status: "ready"})
		case "save":
			reply(w, map[string]any{"draft": client.Draft{HeadSHA: "saved", Status: "ready"}})
		case "commit":
			if !slices.Contains(calls, "POST discard") {
				w.WriteHeader(http.StatusConflict)
				reply(w, map[string]any{"_tag": client.TagDraftConflict, "message": "moved"})
				return
			}
			reply(w, map[string]any{"branch": "main", "commit": "saved"})
		case "discard":
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.New(context.Background(), client.Config{URL: srv.URL, APIKey: "tnzu_fake"})
	if err != nil {
		t.Fatal(err)
	}
	text := "from {x: 1}\n"
	commit, err := publishFile(context.Background(), c, branchTarget{Organization: "org-1", WorkspaceID: "ws-1", Branch: "main"},
		fileWrite{Path: "pipelines/a.tql", Content: &text, Message: "m", Create: true})
	if err != nil || commit != "saved" {
		t.Fatalf("got %q, %v", commit, err)
	}
	want := []string{"GET detail", "POST ", "POST save", "POST commit", "POST discard",
		"GET detail", "POST ", "POST save", "POST commit"}
	if !slices.Equal(calls, want) {
		t.Errorf("calls %q, want %q", calls, want)
	}
}
