-- 229 把存量 glm 账号改成 zhipu，但没有回填 account_mode。
-- 本地合并前 GetGLMOpenAIBaseURL() 默认走 Coding Plan 端点，而网关实际走
-- GetOpenAIBaseURL()：zhipu 且未写 account_mode 时落到按量付费端点。
-- 对「平台已是 zhipu、未显式写 mode/base_url」的存量账号补 coding，避免扣余额或 4xx。

UPDATE accounts
SET credentials = jsonb_set(credentials, '{account_mode}', '"coding"')
WHERE platform = 'zhipu'
  AND COALESCE(credentials->>'account_mode','') = ''
  AND COALESCE(credentials->>'base_url','') = ''
  AND COALESCE(credentials->>'base_url_openai','') = '';
