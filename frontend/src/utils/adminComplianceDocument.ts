const DEFAULT_SITE_NAME = 'Sub2API'

export function renderAdminComplianceDocumentTemplate(content: string, siteName: string): string {
  const normalizedSiteName = siteName.trim() || DEFAULT_SITE_NAME
  return content
    .replace(/\{\{SITE_NAME\}\}/g, normalizedSiteName)
    .replace(/Sub2API/g, normalizedSiteName)
}
