package apicompat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// ResponsesNamespaceName identifies a function child in a Responses namespace.
// It aliases the chat bridge mapping so both native and bridged paths share one
// namespace identity contract.
type ResponsesNamespaceName = NamespacedToolName

// FlattenResponsesNamespaces converts Codex private namespace declarations into
// public Responses function tools and rewrites namespace-qualified request calls.
func FlattenResponsesNamespaces(req map[string]any) (map[string]ResponsesNamespaceName, bool, error) {
	return FlattenResponsesNamespacesExcept(req, nil)
}

// FlattenResponsesNamespacesExcept is FlattenResponsesNamespaces with a set of
// service-owned namespace names that must remain native in the request.
//
// Declarations are read from every carrier the Responses dialects use: the
// standard top-level tools array and the Responses Lite input[].additional_tools
// items. Missing the Lite carrier would leave its namespace declarations intact
// while the call rewrite below still deleted the namespace from history items,
// and the upstream would reject the mismatch with "Missing namespace for
// function_call". Each carrier is flattened in place so the Lite private-tool
// channel keeps carrying its own declarations.
func FlattenResponsesNamespacesExcept(req map[string]any, preserved map[string]bool) (map[string]ResponsesNamespaceName, bool, error) {
	if req == nil {
		return nil, false, nil
	}
	carriers := responsesNamespaceToolCarriers(req)
	if len(carriers) == 0 {
		return nil, false, nil
	}

	declared := make(map[string]bool)
	for _, carrier := range carriers {
		for _, raw := range carrier.tools {
			tool, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			typ := strings.TrimSpace(stringValue(tool["type"]))
			name := strings.TrimSpace(stringValue(tool["name"]))
			if (typ == "function" || typ == "custom") && name != "" {
				declared[name] = true
			}
		}
	}

	names := make(map[string]ResponsesNamespaceName)
	for _, carrier := range carriers {
		for _, raw := range carrier.tools {
			tool, ok := raw.(map[string]any)
			if !ok || strings.TrimSpace(stringValue(tool["type"])) != "namespace" {
				continue
			}
			namespace := strings.TrimSpace(stringValue(tool["name"]))
			if namespace == "" || preserved[namespace] {
				continue
			}
			for _, rawChild := range namespaceChildren(tool) {
				child, ok := rawChild.(map[string]any)
				if !ok || strings.TrimSpace(stringValue(child["type"])) != "function" {
					continue
				}
				name := strings.TrimSpace(stringValue(child["name"]))
				if name == "" {
					continue
				}
				flat := flattenNamespaceToolName(namespace, name)
				entry := ResponsesNamespaceName{Namespace: namespace, Name: name}
				if declared[flat] {
					return nil, false, fmt.Errorf("namespace tool %q/%q flattens to %q which conflicts with a top-level tool of the same name; this upstream cannot disambiguate them, rename one of the tools", namespace, name, flat)
				}
				if prev, exists := names[flat]; exists && prev != entry {
					return nil, false, fmt.Errorf("namespace tools %q/%q and %q/%q both flatten to %q; this upstream cannot disambiguate them, rename one of the tools", prev.Namespace, prev.Name, namespace, name, flat)
				}
				names[flat] = entry
			}
		}
	}
	if len(names) == 0 {
		return nil, false, nil
	}

	// seen spans carriers: the same namespace child declared in both the
	// top-level tools and the Lite carrier must not flatten into two identical
	// declarations the upstream would reject as duplicates.
	seen := make(map[string]bool)
	for _, carrier := range carriers {
		carrier.container["tools"] = flattenResponsesNamespaceCarrier(carrier.tools, preserved, seen)
	}
	rewriteNamespaceQualifiedCalls(req["input"], names)
	if choice, ok := req["tool_choice"].(map[string]any); ok {
		choiceNamespace := strings.TrimSpace(stringValue(choice["name"]))
		if strings.TrimSpace(stringValue(choice["type"])) == "namespace" && !preserved[choiceNamespace] {
			req["tool_choice"] = "auto"
		} else {
			rewriteNamespaceQualifiedCall(choice, names)
		}
	}
	return names, true, nil
}

// responsesToolCarrier is a map owning a "tools" array of declarations.
type responsesToolCarrier struct {
	container map[string]any
	tools     []any
}

// responsesNamespaceToolCarriers returns every declaration carrier in request
// order: top-level tools first, then each input[].additional_tools item.
func responsesNamespaceToolCarriers(req map[string]any) []responsesToolCarrier {
	carriers := make([]responsesToolCarrier, 0, 2)
	if tools, ok := req["tools"].([]any); ok && len(tools) > 0 {
		carriers = append(carriers, responsesToolCarrier{container: req, tools: tools})
	}
	input, ok := req["input"].([]any)
	if !ok {
		return carriers
	}
	for _, rawItem := range input {
		item, ok := rawItem.(map[string]any)
		if !ok || strings.TrimSpace(stringValue(item["type"])) != "additional_tools" {
			continue
		}
		tools, ok := item["tools"].([]any)
		if !ok || len(tools) == 0 {
			continue
		}
		carriers = append(carriers, responsesToolCarrier{container: item, tools: tools})
	}
	return carriers
}

func flattenResponsesNamespaceCarrier(tools []any, preserved, seen map[string]bool) []any {
	flattened := make([]any, 0, len(tools))
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok || strings.TrimSpace(stringValue(tool["type"])) != "namespace" {
			flattened = append(flattened, raw)
			continue
		}
		namespace := strings.TrimSpace(stringValue(tool["name"]))
		if preserved[namespace] {
			flattened = append(flattened, raw)
			continue
		}
		for _, rawChild := range namespaceChildren(tool) {
			child, ok := rawChild.(map[string]any)
			if !ok || strings.TrimSpace(stringValue(child["type"])) != "function" {
				continue
			}
			name := strings.TrimSpace(stringValue(child["name"]))
			flat := flattenNamespaceToolName(namespace, name)
			if name == "" || seen[flat] {
				continue
			}
			seen[flat] = true
			flatChild := make(map[string]any, len(child))
			for key, value := range child {
				flatChild[key] = value
			}
			flatChild["name"] = flat
			flattened = append(flattened, flatChild)
		}
	}
	return flattened
}

// RestoreResponsesNamespaceCalls restores flattened function calls in a JSON
// Responses payload to the namespace/name identity expected by Codex.
func RestoreResponsesNamespaceCalls(payload []byte, names map[string]ResponsesNamespaceName) ([]byte, bool, error) {
	if len(payload) == 0 || len(names) == 0 {
		return payload, false, nil
	}
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return payload, false, err
	}
	changed := restoreResponsesNamespaceValue(value, names)
	if !changed {
		return payload, false, nil
	}
	var rebuilt bytes.Buffer
	encoder := json.NewEncoder(&rebuilt)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return payload, false, err
	}
	return bytes.TrimSuffix(rebuilt.Bytes(), []byte("\n")), true, nil
}

func namespaceChildren(tool map[string]any) []any {
	if children, ok := tool["tools"].([]any); ok && len(children) > 0 {
		return children
	}
	children, _ := tool["children"].([]any)
	return children
}

func rewriteNamespaceQualifiedCalls(value any, names map[string]ResponsesNamespaceName) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			rewriteNamespaceQualifiedCalls(item, names)
		}
	case map[string]any:
		if strings.TrimSpace(stringValue(typed["type"])) == "function_call" {
			rewriteNamespaceQualifiedCall(typed, names)
		}
		for _, child := range typed {
			rewriteNamespaceQualifiedCalls(child, names)
		}
	}
}

func rewriteNamespaceQualifiedCall(item map[string]any, names map[string]ResponsesNamespaceName) bool {
	namespace := strings.TrimSpace(stringValue(item["namespace"]))
	name := strings.TrimSpace(stringValue(item["name"]))
	if namespace == "" || name == "" {
		return false
	}
	flat := flattenNamespaceToolName(namespace, name)
	entry, ok := names[flat]
	if !ok || entry.Namespace != namespace || entry.Name != name {
		return false
	}
	item["name"] = flat
	delete(item, "namespace")
	return true
}

func restoreResponsesNamespaceValue(value any, names map[string]ResponsesNamespaceName) bool {
	changed := false
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			changed = restoreResponsesNamespaceValue(item, names) || changed
		}
	case map[string]any:
		if strings.TrimSpace(stringValue(typed["type"])) == "function_call" {
			if entry, ok := names[strings.TrimSpace(stringValue(typed["name"]))]; ok {
				typed["name"] = entry.Name
				typed["namespace"] = entry.Namespace
				changed = true
			}
		}
		for _, child := range typed {
			changed = restoreResponsesNamespaceValue(child, names) || changed
		}
	}
	return changed
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}
