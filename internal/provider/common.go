package provider

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

// configuredClient extracts the client that the provider's Configure made.
// Terraform configures resources before the provider during validation, so
// providerData may still be nil.
func configuredClient(providerData any, diags *diag.Diagnostics) *client.Client {
	if providerData == nil {
		return nil
	}
	c, ok := providerData.(*client.Client)
	if !ok {
		diags.AddError("Unexpected provider data", fmt.Sprintf("Expected *client.Client, got %T. This is a bug in the provider.", providerData))
		return nil
	}
	return c
}

// splitImportID splits an import ID of the given slash-separated parts.
func splitImportID(id string, parts ...string) ([]string, error) {
	fields := strings.Split(id, "/")
	if len(fields) != len(parts) {
		return nil, fmt.Errorf("expected an import ID of the form %s, got %q", strings.Join(parts, "/"), id)
	}
	for i, f := range fields {
		if f == "" {
			return nil, fmt.Errorf("the %s in import ID %q is empty", parts[i], id)
		}
	}
	return fields, nil
}

// optionalString maps an absent API string to null.
func optionalString(s *string) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

// stringPointer maps null and unknown to nil.
func stringPointer(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}
