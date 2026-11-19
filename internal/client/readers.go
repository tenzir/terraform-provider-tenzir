package client

import (
	"encoding/json"
	"fmt"
)

// ReadableBy says which deployments may read a secret or an ingest endpoint:
// every one the owner reaches, or only those listed. On the wire it is the
// string "everyone" or an object of lists.
type ReadableBy struct {
	Everyone    bool
	Workspaces  []string
	Deployments []string
}

// Everyone lets every deployment read.
var Everyone = ReadableBy{Everyone: true}

func (r ReadableBy) MarshalJSON() ([]byte, error) {
	if r.Everyone {
		return json.Marshal("everyone")
	}
	m := map[string][]string{}
	// Omitted lists default to empty, and leaving out the workspace list
	// keeps the body valid for a workspace secret, which has none.
	if len(r.Workspaces) > 0 {
		m["workspaces"] = r.Workspaces
	}
	if len(r.Deployments) > 0 {
		m["deployments"] = r.Deployments
	}
	return json.Marshal(m)
}

func (r *ReadableBy) UnmarshalJSON(data []byte) error {
	var s string
	if json.Unmarshal(data, &s) == nil {
		if s != "everyone" {
			return fmt.Errorf("unknown readableBy %q", s)
		}
		*r = Everyone
		return nil
	}
	var lists struct {
		Workspaces  []string `json:"workspaces"`
		Deployments []string `json:"deployments"`
	}
	if err := json.Unmarshal(data, &lists); err != nil {
		return err
	}
	*r = ReadableBy{Workspaces: lists.Workspaces, Deployments: lists.Deployments}
	return nil
}
