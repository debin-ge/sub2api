package apicompat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFlattenResponsesNamespaces_RewritesDeclarationHistoryAndChoice(t *testing.T) {
	req := map[string]any{
		"model": "gpt-5.5",
		"tools": []any{
			map[string]any{"type": "function", "name": "plain", "description": "keep"},
			map[string]any{
				"type": "namespace",
				"name": "collaboration",
				"tools": []any{
					map[string]any{"type": "function", "name": "spawn_agent", "description": "spawn", "parameters": map[string]any{"type": "object"}},
				},
			},
		},
		"tool_choice": map[string]any{"type": "function", "name": "spawn_agent", "namespace": "collaboration"},
		"input": []any{
			map[string]any{"type": "function_call", "call_id": "call_1", "name": "spawn_agent", "namespace": "collaboration", "arguments": "{}"},
			map[string]any{"type": "message", "role": "user", "content": "hi", "name": "spawn_agent", "namespace": "collaboration"},
		},
	}

	names, changed, err := FlattenResponsesNamespaces(req)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, ResponsesNamespaceName{Namespace: "collaboration", Name: "spawn_agent"}, names["collaboration__spawn_agent"])

	tools, ok := req["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 2)
	plainTool, ok := tools[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "plain", plainTool["name"])
	flatTool, ok := tools[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "collaboration__spawn_agent", flatTool["name"])
	require.Equal(t, "spawn", flatTool["description"])

	choice, ok := req["tool_choice"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "collaboration__spawn_agent", choice["name"])
	require.NotContains(t, choice, "namespace")

	input, ok := req["input"].([]any)
	require.True(t, ok)
	require.Len(t, input, 2)
	call, ok := input[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "collaboration__spawn_agent", call["name"])
	require.NotContains(t, call, "namespace")
	message, ok := input[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "spawn_agent", message["name"])
	require.Equal(t, "collaboration", message["namespace"])
	require.Equal(t, "gpt-5.5", req["model"])
}

func TestFlattenResponsesNamespaces_RejectsFlatNameCollision(t *testing.T) {
	req := map[string]any{"tools": []any{
		map[string]any{"type": "function", "name": "collaboration__spawn_agent"},
		map[string]any{"type": "namespace", "name": "collaboration", "tools": []any{
			map[string]any{"type": "function", "name": "spawn_agent"},
		}},
	}}

	_, _, err := FlattenResponsesNamespaces(req)
	require.ErrorContains(t, err, "conflicts with a top-level tool")
}

func TestFlattenResponsesNamespaces_NamespaceGroupChoiceFallsBackToAuto(t *testing.T) {
	req := map[string]any{
		"tools": []any{map[string]any{
			"type": "namespace", "name": "collaboration", "tools": []any{
				map[string]any{"type": "function", "name": "spawn_agent"},
				map[string]any{"type": "function", "name": "send_message"},
			},
		}},
		"tool_choice": map[string]any{"type": "namespace", "name": "collaboration"},
	}

	_, changed, err := FlattenResponsesNamespaces(req)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "auto", req["tool_choice"])
}

func TestFlattenResponsesNamespacesExcept_PreservesBuiltInNamespaceAndChoice(t *testing.T) {
	req := map[string]any{
		"tools": []any{
			map[string]any{"type": "namespace", "name": "image_gen", "tools": []any{
				map[string]any{"type": "function", "name": "imagegen"},
			}},
			map[string]any{"type": "namespace", "name": "collaboration", "tools": []any{
				map[string]any{"type": "function", "name": "spawn_agent"},
			}},
		},
		"tool_choice": map[string]any{"type": "namespace", "name": "image_gen"},
	}

	names, changed, err := FlattenResponsesNamespacesExcept(req, map[string]bool{"image_gen": true})
	require.NoError(t, err)
	require.True(t, changed)
	require.Contains(t, names, "collaboration__spawn_agent")
	tools, ok := req["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 2)
	preservedTool, ok := tools[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "namespace", preservedTool["type"])
	require.Equal(t, "image_gen", preservedTool["name"])
	flatTool, ok := tools[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "function", flatTool["type"])
	require.Equal(t, "collaboration__spawn_agent", flatTool["name"])
	require.Equal(t, map[string]any{"type": "namespace", "name": "image_gen"}, req["tool_choice"])
}

func TestFlattenResponsesNamespaces_RejectsNamespaceCollision(t *testing.T) {
	req := map[string]any{"tools": []any{
		map[string]any{"type": "namespace", "name": "a", "tools": []any{
			map[string]any{"type": "function", "name": "b__c"},
		}},
		map[string]any{"type": "namespace", "name": "a__b", "tools": []any{
			map[string]any{"type": "function", "name": "c"},
		}},
	}}

	_, _, err := FlattenResponsesNamespaces(req)
	require.ErrorContains(t, err, "both flatten")
}

func TestRestoreResponsesNamespaceCalls_RewritesOnlyFunctionCalls(t *testing.T) {
	payload := []byte(`{"type":"response.completed","response":{"output":[{"type":"function_call","name":"collaboration__spawn_agent","call_id":"call_1","arguments":"{}","extra":"keep"},{"type":"function_call","name":"plain","arguments":"{}"},{"type":"message","name":"collaboration__spawn_agent","content":"<tag>&value</tag>"}]}}`)
	names := map[string]ResponsesNamespaceName{
		"collaboration__spawn_agent": {Namespace: "collaboration", Name: "spawn_agent"},
	}

	got, changed, err := RestoreResponsesNamespaceCalls(payload, names)
	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `{"type":"response.completed","response":{"output":[{"type":"function_call","name":"spawn_agent","namespace":"collaboration","call_id":"call_1","arguments":"{}","extra":"keep"},{"type":"function_call","name":"plain","arguments":"{}"},{"type":"message","name":"collaboration__spawn_agent","content":"<tag>&value</tag>"}]}}`, string(got))
	require.Contains(t, string(got), "<tag>&value</tag>")
	require.NotContains(t, string(got), `\u003c`)
}

func TestRestoreResponsesNamespaceCalls_RewritesLifecycleItems(t *testing.T) {
	for _, eventType := range []string{"response.output_item.added", "response.output_item.done"} {
		t.Run(eventType, func(t *testing.T) {
			payload := []byte(`{"type":"` + eventType + `","item":{"type":"function_call","name":"collaboration__spawn_agent","arguments":"{}"}}`)
			got, changed, err := RestoreResponsesNamespaceCalls(payload, map[string]ResponsesNamespaceName{
				"collaboration__spawn_agent": {Namespace: "collaboration", Name: "spawn_agent"},
			})
			require.NoError(t, err)
			require.True(t, changed)
			require.JSONEq(t, `{"type":"`+eventType+`","item":{"type":"function_call","name":"spawn_agent","namespace":"collaboration","arguments":"{}"}}`, string(got))
		})
	}
}

// Responses Lite carries private namespace declarations in
// input[].additional_tools. Flattening must rewrite that carrier in place;
// leaving the declaration as a namespace tool while the history call is
// rewritten makes the upstream reject the request.
func TestFlattenResponsesNamespaces_FlattensLiteAdditionalToolsCarrier(t *testing.T) {
	req := map[string]any{
		"tools": []any{map[string]any{"type": "function", "name": "exec"}},
		"input": []any{
			map[string]any{"type": "additional_tools", "tools": []any{
				map[string]any{"type": "namespace", "name": "collaboration", "tools": []any{
					map[string]any{"type": "function", "name": "spawn_agent", "parameters": map[string]any{"type": "object"}},
				}},
			}},
			map[string]any{"type": "function_call", "call_id": "call_1", "name": "spawn_agent", "namespace": "collaboration", "arguments": "{}"},
		},
	}

	names, changed, err := FlattenResponsesNamespaces(req)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, ResponsesNamespaceName{Namespace: "collaboration", Name: "spawn_agent"}, names["collaboration__spawn_agent"])

	input := req["input"].([]any)
	carrier := input[0].(map[string]any)
	carrierTools := carrier["tools"].([]any)
	require.Len(t, carrierTools, 1)
	flatTool := carrierTools[0].(map[string]any)
	require.Equal(t, "function", flatTool["type"])
	require.Equal(t, "collaboration__spawn_agent", flatTool["name"])

	call := input[1].(map[string]any)
	require.Equal(t, "collaboration__spawn_agent", call["name"])
	require.NotContains(t, call, "namespace")

	// Top-level declarations stay untouched.
	tools := req["tools"].([]any)
	require.Len(t, tools, 1)
	require.Equal(t, "exec", tools[0].(map[string]any)["name"])
}

// Preserved namespaces stay native in the Lite carrier too, so their calls keep
// the namespace identity.
func TestFlattenResponsesNamespacesExcept_PreservesLiteCarrierNamespace(t *testing.T) {
	req := map[string]any{
		"tools": []any{map[string]any{"type": "namespace", "name": "collaboration", "tools": []any{
			map[string]any{"type": "function", "name": "spawn_agent"},
		}}},
		"input": []any{
			map[string]any{"type": "additional_tools", "tools": []any{
				map[string]any{"type": "namespace", "name": "image_gen", "tools": []any{
					map[string]any{"type": "function", "name": "generate"},
				}},
			}},
			map[string]any{"type": "function_call", "name": "generate", "namespace": "image_gen", "arguments": "{}"},
		},
	}

	_, changed, err := FlattenResponsesNamespacesExcept(req, map[string]bool{"image_gen": true})
	require.NoError(t, err)
	require.True(t, changed)

	input := req["input"].([]any)
	carrierTool := input[0].(map[string]any)["tools"].([]any)[0].(map[string]any)
	require.Equal(t, "namespace", carrierTool["type"])
	require.Equal(t, "image_gen", carrierTool["name"])
	require.Equal(t, "image_gen", input[1].(map[string]any)["namespace"])
}

// Flat-name collisions must be detected across carriers, not per array.
func TestFlattenResponsesNamespaces_RejectsCollisionAcrossCarriers(t *testing.T) {
	req := map[string]any{
		"tools": []any{map[string]any{"type": "function", "name": "collaboration__spawn_agent"}},
		"input": []any{map[string]any{"type": "additional_tools", "tools": []any{
			map[string]any{"type": "namespace", "name": "collaboration", "tools": []any{
				map[string]any{"type": "function", "name": "spawn_agent"},
			}},
		}}},
	}

	_, _, err := FlattenResponsesNamespaces(req)
	require.ErrorContains(t, err, "conflicts with a top-level tool of the same name")
}

// A request whose declarations live only in the Lite carrier must still flatten
// even though the top-level tools array is absent.
func TestFlattenResponsesNamespaces_FlattensWithoutTopLevelTools(t *testing.T) {
	req := map[string]any{
		"input": []any{map[string]any{"type": "additional_tools", "tools": []any{
			map[string]any{"type": "namespace", "name": "collaboration", "tools": []any{
				map[string]any{"type": "function", "name": "spawn_agent"},
			}},
		}}},
	}

	_, changed, err := FlattenResponsesNamespaces(req)
	require.NoError(t, err)
	require.True(t, changed)
	require.NotContains(t, req, "tools")
	carrierTools := req["input"].([]any)[0].(map[string]any)["tools"].([]any)
	require.Equal(t, "collaboration__spawn_agent", carrierTools[0].(map[string]any)["name"])
}
