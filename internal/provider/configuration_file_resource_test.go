package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
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

// uiDraft calls the draft API of the test user on main, as the web UI's
// editor does, and decodes the answer into out.
func uiDraft(c *client.Client, workspace, method, step string, body, out any) error {
	account, err := c.ReadAccount(context.Background())
	if err != nil {
		return err
	}
	target := fmt.Sprintf("%s/organizations/%s/workspaces/%s/configuration/drafts/%s/%s",
		c.APIURL(), os.Getenv("TENZIR_TEST_ORGANIZATION"), workspace, account.User.ID, step)
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	} else {
		target += "?branch=main"
	}
	req, err := http.NewRequest(method, target, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-tenzir-api-key", os.Getenv("TENZIR_API_KEY"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %d %s", method, step, resp.StatusCode, payload)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(payload, out)
}

type testDraft struct {
	HeadSHA string `json:"headSha"`
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
					text := "from {x: 0}\n"
					var d testDraft
					if err := uiDraft(c, workspace, "POST", "", map[string]string{"branch": "main"}, &d); err != nil {
						t.Fatal(err)
					}
					save := map[string]any{"branch": "main", "headSha": d.HeadSHA, "changes": []client.FileChange{
						{Type: "put", Path: "pipelines/ingest.tql", Text: &text},
					}}
					if err := uiDraft(c, workspace, "POST", "save", save, nil); err != nil {
						t.Fatal(err)
					}
				},
				Config: config("from {x: 3}\n", `"Count to three"`, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(file, "content", "from {x: 3}\n"),
					checkCommit(&c, file, "Count to three"),
					checkDeploymentSeesCommit(&c, file),
					func(*terraform.State) error {
						var d testDraft
						if err := uiDraft(c, workspace, "GET", "detail", nil, &d); err != nil {
							return fmt.Errorf("the web UI's draft is gone: %w", err)
						}
						f, err := c.ReadFile(context.Background(), os.Getenv("TENZIR_TEST_ORGANIZATION"), workspace, d.HeadSHA, "pipelines/ingest.tql")
						if err != nil {
							return err
						}
						if f.Text == nil || *f.Text != "from {x: 0}\n" {
							return fmt.Errorf("the web UI's draft holds %v, not its own edit", f.Text)
						}
						return uiDraft(c, workspace, "POST", "discard", map[string]string{"branch": "main"}, nil)
					},
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
			return nil
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
		d := s.RootModule().Resources["tenzir_deployment.test"].Primary.Attributes
		ref := client.DeploymentRef{Organization: d["organization_id"], Workspace: d["workspace_id"], ID: d["id"]}
		status, err := (*c).ReadDeploymentConfigurationStatus(context.Background(), ref)
		if err != nil {
			return err
		}
		if status.Latest == nil || *status.Latest != commit {
			return fmt.Errorf("the deployment sees %v as the newest commit, not %s", status.Latest, commit)
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
// moved is retried against the head that the refusal names.
func TestPublishFileRetries(t *testing.T) {
	text := "from {x: 1}\n"
	for name, tc := range map[string]struct {
		// moves is how many commits the branch refuses before it takes one.
		moves int
		// there is the content at every head after the first.
		there  *string
		commit string
		calls  []string
		fails  bool
	}{
		"commits at the moved head": {
			moves: 1, commit: "new",
			calls: []string{"commit tip", "file head-1", "commit head-1"},
		},
		"adopts content that arrived meanwhile": {
			moves: 1, there: &text, commit: "head-1",
			calls: []string{"commit tip", "file head-1"},
		},
		"gives up while the branch keeps moving": {
			moves: 5, fails: true,
			calls: []string{"commit tip", "file head-1", "commit head-1", "file head-2", "commit head-2"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			var calls []string
			var srv *httptest.Server
			mux := http.NewServeMux()
			reply := func(w http.ResponseWriter, status int, v any) {
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(v)
			}
			workspace := "/api/organizations/org-1/workspaces/ws-1"
			mux.HandleFunc("GET "+client.WellKnownPath, func(w http.ResponseWriter, r *http.Request) {
				reply(w, http.StatusOK, map[string]any{"apiUrl": srv.URL + "/api"})
			})
			mux.HandleFunc("GET "+workspace+"/file-tree", func(w http.ResponseWriter, r *http.Request) {
				reply(w, http.StatusOK, client.GitTree{Commit: "tip"})
			})
			mux.HandleFunc("GET "+workspace+"/file", func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, "file "+r.URL.Query().Get("commit"))
				if tc.there == nil {
					reply(w, http.StatusNotFound, map[string]any{"_tag": "NotFound"})
					return
				}
				reply(w, http.StatusOK, client.GitFile{Path: "pipelines/a.tql", Text: tc.there})
			})
			moved := 0
			mux.HandleFunc("POST "+workspace+"/branches/{branch}/commits", func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					ExpectedHead string `json:"expectedHead"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				calls = append(calls, "commit "+body.ExpectedHead)
				if moved < tc.moves {
					moved++
					reply(w, http.StatusConflict, map[string]any{"_tag": client.TagHeadMoved, "message": "moved", "head": fmt.Sprintf("head-%d", moved)})
					return
				}
				reply(w, http.StatusCreated, map[string]any{"branch": "main", "commit": "new"})
			})
			srv = httptest.NewServer(mux)
			t.Cleanup(srv.Close)
			c, err := client.New(context.Background(), client.Config{URL: srv.URL, APIKey: "tnzu_fake"})
			if err != nil {
				t.Fatal(err)
			}
			commit, err := publishFile(context.Background(), c, branchTarget{Organization: "org-1", WorkspaceID: "ws-1", Branch: "main"},
				fileWrite{Path: "pipelines/a.tql", Content: &text, Message: "m", Create: true})
			if (err != nil) != tc.fails || commit != tc.commit {
				t.Errorf("got %q, %v", commit, err)
			}
			if !slices.Equal(calls, tc.calls) {
				t.Errorf("calls %q, want %q", calls, tc.calls)
			}
		})
	}
}
