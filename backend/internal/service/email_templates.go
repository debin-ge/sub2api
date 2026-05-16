package service

import (
	"fmt"
	"html"
	"strings"
)

const fallbackEmailSiteName = "平台通知"

type brandedEmailOptions struct {
	SiteName string
	Accent   string
	Label    string
	Title    string
	Body     string
	Footer   string
}

// EmailDisplaySiteName returns the public-facing site name used in outbound mail.
func EmailDisplaySiteName(siteName string) string {
	name := strings.TrimSpace(siteName)
	if name == "" || strings.EqualFold(name, "Sub2API") {
		return fallbackEmailSiteName
	}
	return name
}

// EmailSubjectSiteName returns a site name safe for SMTP headers.
func EmailSubjectSiteName(siteName string) string {
	return sanitizeEmailHeader(EmailDisplaySiteName(siteName))
}

func renderBrandedEmail(opts brandedEmailOptions) string {
	accent := strings.TrimSpace(opts.Accent)
	if accent == "" {
		accent = "#2563eb"
	}
	siteName := html.EscapeString(EmailDisplaySiteName(opts.SiteName))
	label := html.EscapeString(strings.TrimSpace(opts.Label))
	title := html.EscapeString(strings.TrimSpace(opts.Title))
	footer := strings.TrimSpace(opts.Footer)
	if footer == "" {
		footer = fmt.Sprintf("此邮件由 %s 自动发送，请勿回复。", siteName)
	}

	return fmt.Sprintf(`<!doctype html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        body { margin:0; padding:0; background:#f6f8fb; color:#172033; font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Arial,sans-serif; }
        .email-shell { max-width:680px; margin:0 auto; padding:32px 20px; }
        .brand-bar { height:8px; background:%s; border-radius:14px 14px 0 0; }
        .card { background:#fff; border-radius:0 0 14px 14px; padding:40px 48px; box-shadow:0 8px 28px rgba(15,23,42,.08); }
        .site-name { color:%s; font-size:16px; font-weight:700; margin:0 0 18px; }
        .eyebrow { letter-spacing:3px; color:#7c8798; font-size:12px; text-transform:uppercase; margin-bottom:12px; }
        h1 { margin:0 0 24px; font-size:28px; line-height:1.3; color:#111827; }
        p { margin:0 0 14px; font-size:16px; line-height:1.8; color:#4b5563; }
        .code { display:inline-block; margin:18px 0 22px; padding:18px 28px; border-radius:12px; background:#f3f6fb; border:1px solid #dbe3ef; color:#111827; font-size:34px; font-weight:800; letter-spacing:7px; font-family:'SFMono-Regular',Consolas,monospace; }
        .button { display:inline-block; margin:20px 0 22px; padding:13px 28px; border-radius:10px; background:%s; color:#fff !important; text-decoration:none; font-size:16px; font-weight:700; }
        .notice { margin-top:22px; padding:18px 20px; border-radius:12px; background:#f8fafc; border:1px solid #e5eaf2; }
        .warning { color:#b91c1c; font-weight:600; }
        .metric { display:flex; justify-content:space-between; gap:18px; padding:13px 0; border-bottom:1px solid #edf1f7; }
        .metric-label { color:#6b7280; }
        .metric-value { color:#111827; font-weight:700; text-align:right; }
        .footer { margin-top:30px; padding-top:18px; border-top:1px solid #edf1f7; color:#7c8798; font-size:13px; line-height:1.7; }
        @media (max-width:600px) {
            .email-shell { padding:20px 12px; }
            .card { padding:30px 24px; }
            h1 { font-size:24px; }
            .code { font-size:28px; letter-spacing:5px; }
            .metric { display:block; }
            .metric-value { display:block; margin-top:4px; text-align:left; }
        }
    </style>
</head>
<body>
    <div class="email-shell">
        <div class="brand-bar"></div>
        <div class="card">
            <div class="site-name">%s</div>
            <div class="eyebrow">%s</div>
            <h1>%s</h1>
            %s
            <div class="footer">%s</div>
        </div>
    </div>
</body>
</html>`, accent, accent, accent, siteName, label, title, opts.Body, footer)
}

func buildVerifyCodeEmailHTML(code, siteName string) string {
	body := fmt.Sprintf(`
            <p>Your verification code is below. Enter it within 15 minutes to continue.</p>
            <div class="code">%s</div>
            <div class="notice">
                <p>此验证码将在 <strong>15 分钟</strong>后失效。</p>
                <p>If you did not request this code, you can safely ignore this email.</p>
            </div>`, html.EscapeString(code))
	return renderBrandedEmail(brandedEmailOptions{
		SiteName: siteName,
		Accent:   "#2563eb",
		Label:    "Verification / 验证",
		Title:    "邮箱验证码 / Email Verification Code",
		Body:     body,
	})
}

func buildPasswordResetEmailHTML(resetURL, siteName string) string {
	escapedURL := html.EscapeString(resetURL)
	body := fmt.Sprintf(`
            <p>您已请求重置密码。请点击下方按钮设置新密码。</p>
            <p>You requested a password reset. Use the button below to choose a new password.</p>
            <a href="%s" class="button">重置密码 / Reset Password</a>
            <div class="notice">
                <p>此链接将在 <strong>30 分钟</strong>后失效。</p>
                <p class="warning">如果您没有请求重置密码，请忽略此邮件。您的密码将保持不变。</p>
                <p style="word-break:break-all;">如果按钮无法点击，请复制此链接：%s</p>
            </div>`, escapedURL, escapedURL)
	return renderBrandedEmail(brandedEmailOptions{
		SiteName: siteName,
		Accent:   "#2563eb",
		Label:    "Security / 安全",
		Title:    "密码重置请求 / Password Reset",
		Body:     body,
	})
}

func buildNotifyVerifyEmailHTML(code, siteName string) string {
	body := fmt.Sprintf(`
            <p>您正在添加额外的通知邮箱，请输入此验证码完成验证。</p>
            <p>You are adding an extra notification email. Enter this code to verify it.</p>
            <div class="code">%s</div>
            <div class="notice">
                <p>此验证码将在 <strong>15 分钟</strong>后失效。</p>
                <p>If you did not request this code, you can safely ignore this email.</p>
            </div>`, html.EscapeString(code))
	return renderBrandedEmail(brandedEmailOptions{
		SiteName: siteName,
		Accent:   "#2563eb",
		Label:    "Notification / 通知",
		Title:    "通知邮箱验证码 / Notification Email Verification",
		Body:     body,
	})
}

func buildBalanceLowEmailHTML(userName string, balance, threshold float64, siteName, rechargeURL string) string {
	rechargeBlock := ""
	if strings.TrimSpace(rechargeURL) != "" {
		rechargeBlock = fmt.Sprintf(`<a href="%s" class="button">立即充值 / Top Up Now</a>`, html.EscapeString(rechargeURL))
	}
	body := fmt.Sprintf(`
            <p><strong>%s</strong>，您的账户余额已低于提醒阈值。</p>
            <p>Dear %s, your account balance has fallen below the alert threshold.</p>
            <div class="code" style="letter-spacing:0;">$%.2f</div>
            <div class="notice">
                <div class="metric"><span class="metric-label">当前余额 / Current Balance</span><span class="metric-value">$%.2f</span></div>
                <div class="metric"><span class="metric-label">提醒阈值 / Alert Threshold</span><span class="metric-value">$%.2f</span></div>
                <p style="margin-top:16px;">请及时充值以免服务中断。</p>
                <p>Please top up to avoid service interruption.</p>
            </div>
            %s`,
		html.EscapeString(userName),
		html.EscapeString(userName),
		balance,
		balance,
		threshold,
		rechargeBlock,
	)
	return renderBrandedEmail(brandedEmailOptions{
		SiteName: siteName,
		Accent:   "#d97706",
		Label:    "Billing / 计费",
		Title:    "余额不足提醒 / Balance Low Alert",
		Body:     body,
	})
}

func buildQuotaAlertEmailHTML(accountID int64, accountName, platform, dimLabel string, used, limit, remaining float64, thresholdDisplay, siteName string) string {
	limitStr := fmt.Sprintf("$%.2f", limit)
	if limit <= 0 {
		limitStr = "无限制 / Unlimited"
	}
	body := fmt.Sprintf(`
            <p>账号剩余额度已低于提醒阈值，请及时关注。</p>
            <p>Account remaining quota has fallen below the alert threshold.</p>
            <div class="notice">
                <div class="metric"><span class="metric-label">账号 ID / Account ID</span><span class="metric-value">#%d</span></div>
                <div class="metric"><span class="metric-label">账号 / Account</span><span class="metric-value">%s</span></div>
                <div class="metric"><span class="metric-label">平台 / Platform</span><span class="metric-value">%s</span></div>
                <div class="metric"><span class="metric-label">维度 / Dimension</span><span class="metric-value">%s</span></div>
                <div class="metric"><span class="metric-label">已使用 / Used</span><span class="metric-value">$%.2f</span></div>
                <div class="metric"><span class="metric-label">限额 / Limit</span><span class="metric-value">%s</span></div>
                <div class="metric"><span class="metric-label">剩余额度 / Remaining</span><span class="metric-value">$%.2f</span></div>
                <div class="metric"><span class="metric-label">提醒阈值 / Alert Threshold</span><span class="metric-value">%s</span></div>
            </div>`,
		accountID,
		html.EscapeString(accountName),
		html.EscapeString(platform),
		html.EscapeString(dimLabel),
		used,
		html.EscapeString(limitStr),
		remaining,
		html.EscapeString(thresholdDisplay),
	)
	return renderBrandedEmail(brandedEmailOptions{
		SiteName: siteName,
		Accent:   "#dc2626",
		Label:    "Quota / 限额",
		Title:    "账号限额告警 / Account Quota Alert",
		Body:     body,
	})
}

// BuildSMTPTestEmailBody builds the HTML body used by the admin SMTP test endpoint.
func BuildSMTPTestEmailBody(siteName string) string {
	body := `
            <div class="code" style="letter-spacing:0;color:#059669;">OK</div>
            <p>Email configuration successful.</p>
            <p>这是一封用于验证 SMTP 配置是否可用的测试邮件。</p>`
	return renderBrandedEmail(brandedEmailOptions{
		SiteName: siteName,
		Accent:   "#059669",
		Label:    "SMTP Test / 邮件测试",
		Title:    "邮件配置测试成功",
		Body:     body,
		Footer:   "This is an automated test message.",
	})
}
