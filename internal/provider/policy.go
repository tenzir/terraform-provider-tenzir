package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/attr/xattr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// policyGroup is one grant of a policy: actions on what its constraints select.
type policyGroup struct {
	target        any
	relationships []string
	actions       map[string]bool
	key           string
}

// normalizePolicy mirrors the platform's Policy.fromEntries: it merges grants
// with equal constraints and drops actions that a broader grant already
// covers. It returns the policy as canonical JSON, with grants and actions
// sorted, so that two policies the platform stores alike compare equal.
func normalizePolicy(raw string) (string, error) {
	var rules []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return "", fmt.Errorf("a policy is a JSON array of [constraints, actions] pairs: %w", err)
	}
	groups := map[string]*policyGroup{}
	for i, rule := range rules {
		var pair []json.RawMessage
		if err := json.Unmarshal(rule, &pair); err != nil || len(pair) != 2 {
			return "", fmt.Errorf("policy rule %d is not a [constraints, actions] pair", i)
		}
		var constraints struct {
			Target        map[string]any `json:"target"`
			Relationships []string       `json:"relationships"`
		}
		if err := json.Unmarshal(pair[0], &constraints); err != nil || constraints.Target == nil {
			return "", fmt.Errorf("policy rule %d: constraints need a target object and a relationships array", i)
		}
		var actions []string
		if err := json.Unmarshal(pair[1], &actions); err != nil || len(actions) == 0 {
			return "", fmt.Errorf("policy rule %d: actions must be a non-empty array of strings", i)
		}
		relationships := sortedUnique(constraints.Relationships)
		target, err := json.Marshal(constraints.Target)
		if err != nil {
			return "", err
		}
		rels, _ := json.Marshal(relationships)
		key := string(target) + string(rels)
		g, ok := groups[key]
		if !ok {
			g = &policyGroup{target: constraints.Target, relationships: relationships, actions: map[string]bool{}, key: key}
			groups[key] = g
		}
		for _, a := range actions {
			g.actions[a] = true
		}
	}

	var out []any
	for _, g := range groups {
		var remaining []string
		for action := range g.actions {
			covered := false
			for _, other := range groups {
				if other != g && other.actions[action] && other.contains(g) {
					covered = true
					break
				}
			}
			if !covered {
				remaining = append(remaining, action)
			}
		}
		if len(remaining) == 0 {
			continue
		}
		sort.Strings(remaining)
		out = append(out, []any{
			map[string]any{"target": g.target, "relationships": g.relationships},
			remaining,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := json.Marshal(out[i])
		b, _ := json.Marshal(out[j])
		return bytes.Compare(a, b) < 0
	})
	if out == nil {
		out = []any{}
	}
	encoded, err := json.Marshal(out)
	return string(encoded), err
}

// contains reports whether g selects everything that candidate selects.
func (g *policyGroup) contains(candidate *policyGroup) bool {
	if !selectorContains(g.target, candidate.target) {
		return false
	}
	have := map[string]bool{}
	for _, r := range candidate.relationships {
		have[r] = true
	}
	for _, r := range g.relationships {
		if !have[r] {
			return false
		}
	}
	return true
}

// selectorContains follows the platform's Target.contains: a field the
// selector leaves out matches anything, and objects match field by field.
func selectorContains(selector, candidate any) bool {
	if selector == nil {
		return true
	}
	s, sok := selector.(map[string]any)
	c, cok := candidate.(map[string]any)
	if sok && cok {
		for k, v := range s {
			if !selectorContains(v, c[k]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(selector, candidate)
}

func sortedUnique(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// PolicyType is a JSON policy string whose equality follows the platform's
// normalization, since the platform stores policies normalized.
type PolicyType struct {
	basetypes.StringType
}

var _ basetypes.StringTypable = PolicyType{}

func (t PolicyType) Equal(o attr.Type) bool {
	_, ok := o.(PolicyType)
	return ok
}

func (t PolicyType) String() string {
	return "PolicyType"
}

func (t PolicyType) ValueFromString(_ context.Context, in basetypes.StringValue) (basetypes.StringValuable, diag.Diagnostics) {
	return PolicyValue{StringValue: in}, nil
}

func (t PolicyType) ValueFromTerraform(ctx context.Context, in tftypes.Value) (attr.Value, error) {
	v, err := t.StringType.ValueFromTerraform(ctx, in)
	if err != nil {
		return nil, err
	}
	s, ok := v.(basetypes.StringValue)
	if !ok {
		return nil, fmt.Errorf("unexpected value type %T", v)
	}
	return PolicyValue{StringValue: s}, nil
}

func (t PolicyType) ValueType(_ context.Context) attr.Value {
	return PolicyValue{}
}

// PolicyValue is a value of PolicyType.
type PolicyValue struct {
	basetypes.StringValue
}

var (
	_ basetypes.StringValuableWithSemanticEquals = PolicyValue{}
	_ basetypes.StringValuable                   = PolicyValue{}
	_ xattr.ValidateableAttribute                = PolicyValue{}
)

func NewPolicyValue(s string) PolicyValue {
	return PolicyValue{StringValue: basetypes.NewStringValue(s)}
}

func (v PolicyValue) Type(_ context.Context) attr.Type {
	return PolicyType{}
}

func (v PolicyValue) Equal(o attr.Value) bool {
	other, ok := o.(PolicyValue)
	return ok && v.StringValue.Equal(other.StringValue)
}

func (v PolicyValue) StringSemanticEquals(_ context.Context, other basetypes.StringValuable) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics
	o, ok := other.(PolicyValue)
	if !ok {
		diags.AddError("Semantic equality check error", fmt.Sprintf("unexpected value type %T", other))
		return false, diags
	}
	a, err := normalizePolicy(v.ValueString())
	if err != nil {
		return false, nil
	}
	b, err := normalizePolicy(o.ValueString())
	if err != nil {
		return false, nil
	}
	return a == b, nil
}

func (v PolicyValue) ValidateAttribute(_ context.Context, req xattr.ValidateAttributeRequest, resp *xattr.ValidateAttributeResponse) {
	if v.IsNull() || v.IsUnknown() {
		return
	}
	if _, err := normalizePolicy(v.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid policy", err.Error())
	}
}
