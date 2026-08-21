package handler

// CompatibleGatewayHandler is the shared multi-protocol ingress handler.
//
// It is an alias during the incremental rename so existing helper/tests that
// still refer to OpenAIGatewayHandler remain source-compatible while routes
// move to the protocol-neutral CompatibleGateway name.
type CompatibleGatewayHandler = OpenAIGatewayHandler
