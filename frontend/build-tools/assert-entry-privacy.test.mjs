import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'

import { inspectStaticEntryGraph } from './assert-entry-privacy.mjs'

function fixture(files) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'entry-privacy-'))
  for (const [name, contents] of Object.entries(files)) {
    const target = path.join(directory, name)
    fs.mkdirSync(path.dirname(target), { recursive: true })
    fs.writeFileSync(target, contents)
  }
  return directory
}

test('accepts provider code reachable only through a dynamic import', t => {
  const directory = fixture({
    'index.html': '<script type="module" src="/assets/index.js"></script>',
    'assets/index.js': 'import "./shared.js"; import("./stripe.js")',
    'assets/shared.js': 'export const value = 1',
    'assets/stripe.js': 'const sdk = "https://js.stripe.com/v3"',
  })
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }))
  assert.deepEqual(inspectStaticEntryGraph(directory).violations, [])
})

test('rejects payment provider code in the recursively static entry graph', t => {
  const directory = fixture({
    'index.html': '<link rel="modulepreload" href="/assets/vendor.js"><script type="module" src="/assets/index.js"></script>',
    'assets/index.js': 'export{value}from"./shared.js"',
    'assets/shared.js': 'const stripe = "https://js.stripe.com/v3"; export const value = 1',
    'assets/vendor.js': 'const sdk = "https://static-demo.airwallex.com/widgets/sdk.js"',
  })
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }))
  assert.deepEqual(inspectStaticEntryGraph(directory).violations, [
    { marker: 'airwallex.com', module: 'assets/vendor.js' },
    { marker: 'js.stripe.com', module: 'assets/shared.js' },
  ])
})

test('resolves bare-relative HTML roots and rejects unresolved bare module imports', t => {
  const directory = fixture({
    'index.html': '<script type="module" src="assets/index.js"></script>',
    'assets/index.js': 'import "unresolved-package"',
  })
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }))
  assert.deepEqual(inspectStaticEntryGraph(directory).violations, [
    { marker: 'unresolved_static_module', module: 'unresolved-package' },
  ])
})

test('fails closed when module roots are missing or external', t => {
  const missing = fixture({ 'index.html': '<main>no application entry</main>' })
  const external = fixture({
    'index.html': '<script type="module" src="https://cdn.example.invalid/app.js"></script>',
  })
  t.after(() => {
    fs.rmSync(missing, { recursive: true, force: true })
    fs.rmSync(external, { recursive: true, force: true })
  })
  assert.deepEqual(inspectStaticEntryGraph(missing).violations, [
    { marker: 'missing_module_root', module: 'index.html' },
  ])
  assert.deepEqual(inspectStaticEntryGraph(external).violations, [
    { marker: 'external_module_root', module: 'https://cdn.example.invalid/app.js' },
  ])
})
