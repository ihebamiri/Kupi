// Package schema turns a cluster's OpenAPI v3 document into a compact field
// tree that the UI renders as a form. It works for every type the server
// knows, including custom resources, because it reads the server's own schema.
package schema

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	maxDepth          = 14
	maxDescriptionLen = 600
	quantityRef       = "io.k8s.apimachinery.pkg.api.resource.Quantity"
	intOrStringRef    = "io.k8s.apimachinery.pkg.util.intstr.IntOrString"
	timeRef           = "io.k8s.apimachinery.pkg.apis.meta.v1.Time"
	objectMetaRef     = "io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta"
)

// Node is one field in the form tree.
type Node struct {
	// Type is object, array, map, string, integer, number, boolean,
	// intOrString, quantity or any.
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Format      string   `json:"format,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Fields      []Field  `json:"fields,omitempty"`
	Items       *Node    `json:"items,omitempty"`
	Values      *Node    `json:"values,omitempty"`
	// Truncated marks a subtree cut for recursion or depth; edit it as YAML.
	Truncated bool `json:"truncated,omitempty"`
}

// Field is a named property of an object node.
type Field struct {
	Name     string `json:"name"`
	Required bool   `json:"required,omitempty"`
	Node     *Node  `json:"node"`
}

type raw struct {
	Ref                  string          `json:"$ref"`
	AllOf                []raw           `json:"allOf"`
	Type                 string          `json:"type"`
	Format               string          `json:"format"`
	Description          string          `json:"description"`
	Enum                 []interface{}   `json:"enum"`
	Properties           map[string]raw  `json:"properties"`
	Required             []string        `json:"required"`
	Items                *raw            `json:"items"`
	AdditionalProperties json.RawMessage `json:"additionalProperties"`
	IntOrString          bool            `json:"x-kubernetes-int-or-string"`
	PreserveUnknown      bool            `json:"x-kubernetes-preserve-unknown-fields"`
	GVK                  []struct {
		Group   string `json:"group"`
		Version string `json:"version"`
		Kind    string `json:"kind"`
	} `json:"x-kubernetes-group-version-kind"`
}

type document struct {
	Components struct {
		Schemas map[string]raw `json:"schemas"`
	} `json:"components"`
}

// ErrKindNotFound means the document has no schema for the requested kind.
var ErrKindNotFound = errors.New("no schema for this kind")

// Build extracts the form tree for group/version/kind from an OpenAPI v3
// document. Server-managed parts (status, most of metadata) are left out.
func Build(doc []byte, group, version, kind string) (*Node, error) {
	var d document
	if err := json.Unmarshal(doc, &d); err != nil {
		return nil, fmt.Errorf("parse OpenAPI document: %w", err)
	}
	root := ""
	for name, s := range d.Components.Schemas {
		for _, g := range s.GVK {
			if g.Group == group && g.Version == version && g.Kind == kind {
				root = name
			}
		}
	}
	if root == "" {
		return nil, ErrKindNotFound
	}
	b := builder{schemas: d.Components.Schemas}
	node := b.build(raw{Ref: "#/components/schemas/" + root}, nil, 0)
	trimRoot(node)
	return node, nil
}

type builder struct {
	schemas map[string]raw
}

func refName(ref string) string {
	return strings.TrimPrefix(ref, "#/components/schemas/")
}

func (b *builder) build(s raw, stack []string, depth int) *Node {
	// Kubernetes wraps references in allOf to attach defaults; unwrap them.
	if s.Ref == "" && len(s.AllOf) == 1 {
		inner := s.AllOf[0]
		if s.Description != "" && inner.Description == "" {
			inner.Description = s.Description
		}
		s = mergeDescription(inner, s.Description)
	}
	if s.Ref != "" {
		name := refName(s.Ref)
		desc := s.Description
		switch name {
		case quantityRef:
			return &Node{Type: "quantity", Description: clip(desc)}
		case intOrStringRef:
			return &Node{Type: "intOrString", Description: clip(desc)}
		case timeRef:
			return &Node{Type: "string", Format: "date-time", Description: clip(desc)}
		}
		for _, seen := range stack {
			if seen == name {
				return &Node{Type: "any", Description: clip(desc), Truncated: true}
			}
		}
		target, ok := b.schemas[name]
		if !ok {
			return &Node{Type: "any", Description: clip(desc), Truncated: true}
		}
		if desc != "" {
			target.Description = desc
		}
		return b.build(target, append(stack, name), depth)
	}
	if depth > maxDepth {
		return &Node{Type: "any", Description: clip(s.Description), Truncated: true}
	}
	n := &Node{Description: clip(s.Description), Format: s.Format}
	for _, e := range s.Enum {
		if str, ok := e.(string); ok {
			n.Enum = append(n.Enum, str)
		}
	}
	switch {
	case s.IntOrString:
		n.Type = "intOrString"
	case s.Type == "array":
		n.Type = "array"
		if s.Items != nil {
			n.Items = b.build(*s.Items, stack, depth+1)
		} else {
			n.Items = &Node{Type: "any"}
		}
	case len(s.Properties) > 0:
		n.Type = "object"
		required := map[string]bool{}
		for _, r := range s.Required {
			required[r] = true
		}
		names := make([]string, 0, len(s.Properties))
		for name := range s.Properties {
			names = append(names, name)
		}
		sort.Slice(names, func(i, j int) bool {
			if required[names[i]] != required[names[j]] {
				return required[names[i]]
			}
			return names[i] < names[j]
		})
		for _, name := range names {
			n.Fields = append(n.Fields, Field{Name: name, Required: required[name], Node: b.build(s.Properties[name], stack, depth+1)})
		}
	case len(s.AdditionalProperties) > 0 && string(s.AdditionalProperties) != "false":
		n.Type = "map"
		var values raw
		if err := json.Unmarshal(s.AdditionalProperties, &values); err == nil {
			n.Values = b.build(values, stack, depth+1)
		} else {
			n.Values = &Node{Type: "any"}
		}
	case s.Type == "object" || s.PreserveUnknown:
		n.Type = "any"
	case s.Type == "string" || s.Type == "integer" || s.Type == "number" || s.Type == "boolean":
		n.Type = s.Type
	default:
		n.Type = "any"
	}
	return n
}

func mergeDescription(s raw, desc string) raw {
	if desc != "" {
		s.Description = desc
	}
	return s
}

// trimRoot drops the parts users never edit: status, and metadata fields
// the server manages.
func trimRoot(root *Node) {
	keepMeta := map[string]bool{"name": true, "namespace": true, "labels": true, "annotations": true, "generateName": true}
	var fields []Field
	for _, f := range root.Fields {
		switch f.Name {
		case "status":
			continue
		case "metadata":
			if f.Node != nil {
				var meta []Field
				for _, mf := range f.Node.Fields {
					if keepMeta[mf.Name] {
						meta = append(meta, mf)
					}
				}
				f.Node.Fields = meta
				f.Node.Truncated = false
				f.Node.Type = "object"
			}
		}
		fields = append(fields, f)
	}
	root.Fields = fields
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxDescriptionLen {
		return s
	}
	cut := s[:maxDescriptionLen]
	if i := strings.LastIndex(cut, " "); i > maxDescriptionLen/2 {
		cut = cut[:i]
	}
	return cut + "…"
}
