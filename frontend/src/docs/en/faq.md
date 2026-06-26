# Frequently Asked Questions

This page summarizes the most common questions and solutions encountered by users integrating and using {{SITE_NAME}}.

---

## What Should I Fill in for the Base URL?

> **Q: What Should I Fill in for the Base URL?**
>
> **A:** Generally, fill in the root deployment URL of your {{SITE_NAME}} instance, for example:
> ```text
> {{BASE_URL}}
> ```
> If the SDK you are using (such as the OpenAI SDK) requires a standard OpenAI-style `baseURL`, you typically need to point it to the `/v1` endpoint:
> ```text
> {{BASE_URL}}v1
> ```
> *Note: If your administrator provided a custom URL with a specific sub-path, please use the exact full URL they provided, and be careful not to duplicate `/v1` in your client configurations.*

---

## Which HTTP Header Does the API Key Go In?

> **Q: Which HTTP Header Does the API Key Go In?**
>
> **A:** The platform recommends using the standard Bearer Token format in the HTTP header:
> ```http
> Authorization: Bearer $YOUR_KEY
> ```
> For example, in a `curl` command:
> ```bash
> curl "${BASE_URL}v1/models" \
>   -H "Authorization: Bearer $YOUR_KEY"
> ```
> Some compatible clients might use `api-key`, `x-api-key`, or specific SDK configuration properties. Unless your administrator or a specific client explicitly requires otherwise, we strongly recommend prioritizing the standard `Authorization: Bearer` header.

---

## Why Are the Results of `/v1/models` Different for Me and Others?

> **Q: Why Are the Results of `/v1/models` Different for Me and Others?**
>
> **A:** The `/v1/models` endpoint returns the list of models that are **actually authorized and available** for your specific API Key.
>
> The platform segments users and keys into different **groups**. The list of available models for each group can vary depending on upstream accounts assigned by the admin, custom model mappings, channel pricing rates, and quota settings.
>
> > [!IMPORTANT]
> > If you notice a model is missing from your available list, this is typically not a frontend rendering issue. Please verify whether the group associated with your current API Key has been granted access to that model, and if at least one active channel is configured for it in the backend.

---

## Which Endpoint Should I Choose?

> **Q: Which Endpoint Should I Choose?**
>
> **A:** You should choose the access path that best matches the client ecosystem or specific integration needs of your application.

| Client or Scenario | Recommended Endpoint Path |
| :--- | :--- |
| **OpenAI Chat Completions Compatible Clients** | `/v1/chat/completions` |
| **OpenAI Responses or Codex-based Clients** | `/v1/responses` |
| **Claude Code or Anthropic Compatible Clients** | `/v1/messages` |
| **Gemini Native Clients** | `/v1beta/models/{model}:generateContent` |
| **Antigravity Dedicated Clients** | `/antigravity/...` |
| **Text Vectorization (Embeddings)** | `/v1/embeddings` |
| **Image Generation and Editing (DALL-E)** | `/v1/images/generations`, `/v1/images/edits` |

> [!CAUTION]
> The request body format must match the endpoint path exactly. For example, do not send an OpenAI `messages` request format directly to the Gemini native endpoint, and vice versa.

---

## Why Am I Getting a 404 Error?

> **Q: Why Am I Getting a 404 Error?**
>
> **A:** A 404 status code usually indicates a route mismatch or that an endpoint is disabled. Use the table below to check common causes.

| 404 Common Cause | Verification and Fixes |
| :--- | :--- |
| **Base URL configuration mismatch** | Check if your SDK automatically appends `/v1`, resulting in duplicate paths (like `/v1/v1/...`), or if you are missing `/antigravity`, `/v1beta`, or other required path segments. |
| **Endpoint disabled** | The current deployment instance might not have features like image generation, embeddings, or responses enabled. Please check with your administrator. |
| **Model name spelling error** | The model name you requested does not exist in the active backend model list. Make sure to use one of the names returned by the `/v1/models` endpoint. |
| **API Protocol mismatch** | Ensure that your request body structure and target endpoint path conform to the same API specification protocol. |

---

## Why Is the Model Call Failing?

> **Q: Why Is the Model Call Failing?**
>
> **A:** A model call failure (such as getting a 403, 429, or 5xx error) does not necessarily mean the {{SITE_NAME}} service itself is down.

Common reasons for failures include:
1. The group bound to your API Key does not have permissions for the requested model.
2. The upstream account associated with the model in the backend has run out of funds, has an invalid key, or is rate-limited/blocked.
3. The administrator has not configured a mapping rule or channel pricing multiplier for the model.
4. Your request parameters (like the range of `temperature` or `max_tokens`) are not supported by the target upstream model.
5. The streaming response was blocked or terminated prematurely by reverse proxies or gateway timeout settings.

> [!TIP]
> When a call fails, we recommend running this minimal check command to isolate the problem:
> ```bash
> curl "${BASE_URL}v1/models" \
>   -H "Authorization: Bearer $YOUR_KEY"
> ```
> If the minimal request also fails, gather your **request timestamp, request path, model name, the last 4 characters of your key (masked), and error logs**, and send them to your admin for help.
