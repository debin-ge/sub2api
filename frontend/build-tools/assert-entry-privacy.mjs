import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const FORBIDDEN_PROVIDER_MARKERS = [
  'airwallex.com',
  'js.stripe.com',
]

function moduleReferences(source) {
  const references = new Set()
  const patterns = [
    /\bimport\s*(?:[^"'()]*?\bfrom\s*)?["']([^"']+)["']/g,
    /\bexport\s*[^"']*?\bfrom\s*["']([^"']+)["']/g,
  ]
  for (const pattern of patterns) {
    for (const match of source.matchAll(pattern)) references.add(match[1])
  }
  return [...references]
}

function htmlModuleRoots(html) {
  const roots = new Set()
  for (const match of html.matchAll(/<(?:script|link)\b[^>]*>/gi)) {
    const tag = match[0]
    const isModule = /\btype\s*=\s*["']module["']/i.test(tag) ||
      /\brel\s*=\s*["'][^"']*\bmodulepreload\b[^"']*["']/i.test(tag)
    if (!isModule) continue
    const reference = tag.match(/\b(?:src|href)\s*=\s*["']([^"']+)["']/i)?.[1]
    if (reference) roots.add(reference)
  }
  return [...roots]
}

function resolveLocalReference(reference, importer, outputDirectory, allowBareRelative = false) {
  if (reference.startsWith('/')) return path.join(outputDirectory, reference.slice(1))
  if (reference.startsWith('./') || reference.startsWith('../')) return path.resolve(path.dirname(importer), reference)
  if (allowBareRelative && !isExternalReference(reference)) return path.resolve(path.dirname(importer), reference)
  return null
}

function isExternalReference(reference) {
  return reference.startsWith('//') || /^[a-z][a-z\d+.-]*:/i.test(reference)
}

export function inspectStaticEntryGraph(outputDirectory) {
  const indexPath = path.join(outputDirectory, 'index.html')
  const html = fs.readFileSync(indexPath, 'utf8')
  const roots = htmlModuleRoots(html)
  const queue = roots.map(reference => resolveLocalReference(reference, indexPath, outputDirectory, true))
  const visited = new Set()
  const violations = []

  if (roots.length === 0) violations.push({ marker: 'missing_module_root', module: 'index.html' })
  for (const marker of FORBIDDEN_PROVIDER_MARKERS) {
    if (html.includes(marker)) violations.push({ marker, module: 'index.html' })
  }
  for (const reference of roots) {
    if (isExternalReference(reference)) violations.push({ marker: 'external_module_root', module: reference })
    else if (!resolveLocalReference(reference, indexPath, outputDirectory, true)) {
      violations.push({ marker: 'unresolved_module_root', module: reference })
    }
  }

  while (queue.length > 0) {
    const modulePath = queue.shift()
    if (!modulePath || visited.has(modulePath)) continue
    if (!modulePath.startsWith(`${path.resolve(outputDirectory)}${path.sep}`)) {
      throw new Error(`entry graph escapes build output: ${modulePath}`)
    }
    const source = fs.readFileSync(modulePath, 'utf8')
    visited.add(modulePath)
    for (const marker of FORBIDDEN_PROVIDER_MARKERS) {
      if (source.includes(marker)) violations.push({ marker, module: path.relative(outputDirectory, modulePath) })
    }
    for (const reference of moduleReferences(source)) {
      const resolved = resolveLocalReference(reference, modulePath, outputDirectory)
      if (resolved) queue.push(resolved)
      else if (isExternalReference(reference)) {
        violations.push({ marker: 'external_static_module', module: reference })
      } else {
        violations.push({ marker: 'unresolved_static_module', module: reference })
      }
    }
  }

  return { modules: [...visited].map(item => path.relative(outputDirectory, item)).sort(), violations }
}

function main() {
  const frontendDirectory = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
  const outputDirectory = path.resolve(frontendDirectory, '../backend/internal/web/dist')
  const result = inspectStaticEntryGraph(outputDirectory)
  if (result.violations.length > 0) {
    process.stderr.write(`${JSON.stringify({ status: 'fail', failure: 'payment_provider_in_static_entry_graph', ...result })}\n`)
    process.exitCode = 1
    return
  }
  process.stdout.write(`${JSON.stringify({ status: 'pass', static_modules: result.modules.length })}\n`)
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main()
