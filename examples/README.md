# mcp-go Examples

This directory contains runnable examples demonstrating mcp-go features.

## Examples

### [minimal](./minimal/)
The simplest possible MCP server with a single tool. Start here to understand the basics.

```bash
go run ./examples/minimal
```

### [basic](./basic/)
A comprehensive example showing tools, resources, and prompts together.

```bash
go run ./examples/basic
```

### [http](./http/)
HTTP+SSE transport for web-based integrations.

```bash
go run ./examples/http
# Server starts at http://localhost:8080
```

### [middleware](./middleware/)
Using built-in middleware for logging, recovery, and timeouts.

```bash
go run ./examples/middleware
```

### [resources](./resources/)
Resource handlers with URI templates and parameters.

```bash
go run ./examples/resources
```

### [prompts](./prompts/)
Prompt templates with arguments for generating structured prompts.

```bash
go run ./examples/prompts
```

### [session](./session/)
v1.1 features: sampling (LLM requests), roots (workspace awareness), and logging.

```bash
go run ./examples/session
```

## Running Examples

All examples use stdio transport by default (except `http`). To test with Claude Desktop or other MCP clients:

1. Build the example:
   ```bash
   go build -o my-server ./examples/basic
   ```

2. Add to your MCP client configuration:
   ```json
   {
     "mcpServers": {
       "my-server": {
         "command": "/path/to/my-server"
       }
     }
   }
   ```

## Testing Locally

Use the included test client to interact with examples:

```bash
# In one terminal
go run ./examples/basic

# In another terminal (if using test harness)
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"test","version":"1.0"}}}' | go run ./examples/basic
```
