package provider

import (
	"context"
	"testing"
)

func TestPolicySemanticEquality(t *testing.T) {
	cases := []struct {
		name  string
		a, b  string
		equal bool
	}{
		{
			// What the dev platform stored for the first policy.
			name: "server normalization",
			a: `[[{"target":{"_tag":"Workspace"},"relationships":[]},["Workspace.read","Workspace.read"]],
			     [{"target":{"_tag":"Workspace","orgId":"o"},"relationships":[]},["Workspace.read","Workspace.update"]]]`,
			b: `[[{"target":{"_tag":"Workspace","orgId":"o"},"relationships":[]},["Workspace.update"]],
			     [{"target":{"_tag":"Workspace"},"relationships":[]},["Workspace.read"]]]`,
			equal: true,
		},
		{
			name:  "merged constraints and reordered relationships",
			a:     `[[{"target":{"_tag":"User"},"relationships":["self","owner"]},["a"]],[{"target":{"_tag":"User"},"relationships":["owner","self"]},["b"]]]`,
			b:     `[[{"relationships":["owner","self"],"target":{"_tag":"User"}},["b","a"]]]`,
			equal: true,
		},
		{
			name:  "a relationship narrows a grant",
			a:     `[[{"target":{"_tag":"User"},"relationships":["self"]},["a"]]]`,
			b:     `[[{"target":{"_tag":"User"},"relationships":[]},["a"]]]`,
			equal: false,
		},
		{
			name:  "a narrower grant is not covered by a different tag",
			a:     `[[{"target":{"_tag":"Workspace"},"relationships":[]},["a"]],[{"target":{"_tag":"Deployment","orgId":"o"},"relationships":[]},["a"]]]`,
			b:     `[[{"target":{"_tag":"Workspace"},"relationships":[]},["a"]]]`,
			equal: false,
		},
		{
			name: "nested API-key owner selectors",
			a: `[[{"target":{"_tag":"ApiKey","owner":{"_tag":"User"}},"relationships":[]},["ApiKey.read"]],
			     [{"target":{"_tag":"ApiKey","owner":{"_tag":"User","userId":"u"},"apiKeyId":"k"},"relationships":[]},["ApiKey.read"]]]`,
			b:     `[[{"target":{"_tag":"ApiKey","owner":{"_tag":"User"}},"relationships":[]},["ApiKey.read"]]]`,
			equal: true,
		},
		{
			name: "API-key owner kinds stay distinct",
			a: `[[{"target":{"_tag":"ApiKey","owner":{"_tag":"User"}},"relationships":[]},["ApiKey.read"]],
			     [{"target":{"_tag":"ApiKey","owner":{"_tag":"Organization","orgId":"o"}},"relationships":[]},["ApiKey.read"]]]`,
			b: `[[{"target":{"_tag":"ApiKey","owner":{"_tag":"User"}},"relationships":[]},["ApiKey.read"]]]`,
		},
		{
			name:  "empty",
			a:     `[]`,
			b:     ` [ ] `,
			equal: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, diags := NewPolicyValue(tc.a).StringSemanticEquals(context.Background(), NewPolicyValue(tc.b))
			if diags.HasError() {
				t.Fatal(diags)
			}
			if got != tc.equal {
				t.Errorf("got %v, want %v", got, tc.equal)
			}
		})
	}
}

func TestPolicyRejectsMalformedRules(t *testing.T) {
	for _, raw := range []string{`{}`, `[["x"]]`, `[[{"relationships":[]},["a"]]]`, `[[{"target":{"_tag":"User"},"relationships":[]},[]]]`} {
		if _, err := normalizePolicy(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}
