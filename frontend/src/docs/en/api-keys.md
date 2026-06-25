# API Keys and Accounts

The documentation can be viewed publicly, but API requests to {{SITE_NAME}} require a valid API Key. The key determines which models, endpoints, group quotas, and pricing rules you can use, so confirm the key source, permission scope, and storage method before integrating.

---

## Login and API Access

| Scenario | Login Required | Notes |
| :--- | :--- | :--- |
| **Read documentation** | No | Docs are intended to help users integrate and can be viewed without signing in. |
| **Open the dashboard** | Yes | The dashboard is usually used for account, key, usage, or admin information. |
| **Call the API** | API Key required | API requests authenticate through headers and do not use the browser session. |
| **Switch models or endpoints** | Key permission required | Access depends on the API Key group and admin configuration. |

> [!IMPORTANT]
> **About 401 Errors**: If you are signed in to the dashboard but command-line requests still return `401 Unauthorized`, the request is not sending the correct API Key. **Browser login state is not automatically attached to API calls.**

---

## Getting an API Key

API Keys usually come from an admin or the dashboard key page. After receiving a key, confirm:
1. The key is enabled.
2. The key belongs to the right user or project.
3. The key is bound to a valid group.
4. The group exposes the models and endpoints you need.
5. Quota, balance, expiration, or rate limits do not block the request.

---

## Recommended Environment Variables

These docs use the following environment variables:

```bash
export BASE_URL="{{BASE_URL}}"
export YOUR_KEY="replace-with-your-api-key"
```

Requests then use:

```bash
curl "${BASE_URL}v1/models" \
  -H "Authorization: Bearer $YOUR_KEY"
```

If an SDK expects the Base URL at the `/v1` level, configure a separate value:

```bash
export OPENAI_BASE_URL="{{BASE_URL}}v1"
export OPENAI_API_KEY="$YOUR_KEY"
```

---

## Authorization Header

Use Bearer Token authentication by default:

```http
Authorization: Bearer $YOUR_KEY
```

> [!WARNING]
> Please pay attention to avoid the following common **Authorization header configuration mistakes**:

| Mistake Example | Problem | Action |
| :--- | :--- | :--- |
| `Authorization: $YOUR_KEY` | Missing the `Bearer ` prefix. | Append the prefix `Bearer ` followed by a space. |
| `Bearer YOUR_KEY` | Sends a literal string instead of reading the environment variable. | Ensure the environment variable is resolved dynamically. |
| `Authorization: Bearer` | The key is empty, usually because the environment variable is unset. | Check if the environment variable is properly exported. |
| **Hard-coding the key in browser code** | Leaks the key and is not suitable for production. | Never expose API Keys directly on the client side. |

---

## Permissions and Model Lists

Different API Keys in the same deployment may see different model lists. Always query with the key you will use:

```bash
curl "${BASE_URL}v1/models" \
  -H "Authorization: Bearer $YOUR_KEY"
```

> [!TIP]
> If an expected model is missing from the list, check:
> 1. Whether the key belongs to the right user group.
> 2. Whether the group exposes that model.
> 3. Whether an admin configured the model mapping.
> 4. Whether the corresponding channel is enabled and healthy.

---

## Key Security

| Practice | Notes |
| :--- | :--- |
| **Store keys in server env variables** | Do not write real keys into frontend code, mobile bundles, or public repositories. |
| **Split keys by project** | Use separate keys for different apps, environments, or teams to simplify revocation and debugging. |
| **Rotate regularly** | Rotate when a member leaves, a repository leaks, or an environment is migrated. |
| **Use least privilege** | Expose only the models and endpoints the business needs. |
| **Redact logs** | Log request IDs, models, and errors, but never the full key. |

---

## Rotating a Key

A safe rotation usually follows this order:
1. Create a new API Key.
2. Switch the test environment to the new key, then verify `/v1/models` and a minimal request.
3. Update production configuration.
4. Watch success rate, latency, and usage.
5. Disable the old API Key after old traffic stops.

> [!CAUTION]
> If you suspect a key has leaked, **disable or delete it immediately** in the dashboard to avoid financial losses, then investigate access logs and abnormal usage.

---

## Self-Check Command

Use this as the smallest key availability check:

```bash
test -n "$YOUR_KEY" && \
curl -i "${BASE_URL}v1/models" \
  -H "Authorization: Bearer $YOUR_KEY"
```

The expected result is `HTTP/1.1 200 OK` with the model list available to the current key. If you receive 401, 403, or an empty list, continue with the troubleshooting page.
