#!/usr/bin/env node

import { readdir, readFile } from 'node:fs/promises'
import path from 'node:path'
import process from 'node:process'

const engineUrl = (process.env.ENGINE_URL ?? '').replace(/\/+$/, '')
const token = process.env.ENGINE_TOKEN?.trim()
const legacyRoot = process.env.LEGACY_SITE_DIR
const featureBuildDir = process.env.FEATURE_BUILD_DIR

if (!engineUrl || !token || !legacyRoot) {
  throw new Error('Set ENGINE_URL, ENGINE_TOKEN, and LEGACY_SITE_DIR before migration.')
}

const legacyDir = path.resolve(legacyRoot)
const dataDir = path.join(legacyDir, 'backend', 'data')
const assetsDir = path.join(legacyDir, 'backend', 'assets')
const archivesDir = path.resolve(featureBuildDir ?? path.join(legacyDir, 'frontend', 'dist', 'feature-builds'))
const adminHeaders = { Authorization: `Bearer ${token}`, Accept: 'application/json' }

async function readLegacy(name) {
  return JSON.parse(await readFile(path.join(dataDir, name), 'utf8'))
}

async function request(route, options = {}) {
  const response = await fetch(`${engineUrl}${route}`, {
    ...options,
    headers: {
      ...adminHeaders,
      ...(options.headers ?? {}),
    },
  })
  const raw = await response.text()
  let body
  try { body = raw ? JSON.parse(raw) : undefined } catch { body = raw }
  if (!response.ok) {
    const message = body?.error?.message ?? body?.message ?? raw ?? response.statusText
    throw new Error(`${options.method ?? 'GET'} ${route} (${response.status}): ${message}`)
  }
  return body
}

async function listAll(route) {
  const items = []
  let cursor
  do {
    const url = new URL(route, engineUrl)
    url.searchParams.set('limit', '200')
    if (cursor) url.searchParams.set('cursor', cursor)
    const result = await request(url.pathname + url.search)
    items.push(...(result.items ?? []))
    cursor = result.next_cursor || result.nextCursor || undefined
  } while (cursor)
  return items
}

async function uploadFeatureBuilds() {
  const files = (await readdir(archivesDir)).filter((name) => name.endsWith('.zip')).sort()
  if (files.length === 0) throw new Error(`No feature archives found in ${archivesDir}; build the site features first.`)
  for (const name of files) {
    const archive = await readFile(path.join(archivesDir, name))
    const imported = await request('/api/v2/admin/feature-builds', {
      method: 'POST',
      headers: { 'Content-Type': 'application/zip' },
      body: archive,
    })
    process.stdout.write(`feature ${imported.key}@${imported.version} uploaded (${imported.digest.slice(0, 12)})\n`)
  }
}

async function uploadLegacyMedia() {
  const files = (await readdir(assetsDir, { withFileTypes: true }))
    .filter((entry) => entry.isFile() && !entry.name.toLowerCase().endsWith('.woff2') && !entry.name.endsWith('.txt'))
    .map((entry) => entry.name)
    .sort()
  const existing = await listAll('/api/v1/admin/media?status=ready&type=image')
  const byFilename = new Map(existing.map((media) => [media.original_filename, media.id]))
  const ids = new Map()
  for (const filename of files) {
    let id = byFilename.get(filename)
    if (!id) {
      const bytes = await readFile(path.join(assetsDir, filename))
      const ext = path.extname(filename).toLowerCase()
      const contentType = ext === '.svg' ? 'image/svg+xml' : undefined
      const form = new FormData()
      form.set('file', new Blob([bytes], contentType ? { type: contentType } : undefined), filename)
      const uploaded = await request('/api/v1/admin/media', { method: 'POST', body: form })
      id = uploaded.id
      process.stdout.write(`media ${filename} uploaded (${id})\n`)
    } else {
      process.stdout.write(`media ${filename} reused (${id})\n`)
    }
    ids.set(filename, id)
  }
  return ids
}

function mediaID(ids, reference) {
  if (typeof reference !== 'string' || !reference.startsWith('/media/')) return reference
  const filename = decodeURIComponent(reference.slice('/media/'.length))
  const id = ids.get(filename)
  if (!id) throw new Error(`Legacy media reference ${reference} has no uploaded asset.`)
  return id
}

function replaceMediaRefs(value, ids) {
  if (Array.isArray(value)) return value.map((item) => replaceMediaRefs(item, ids))
  if (value && typeof value === 'object') {
    return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, replaceMediaRefs(item, ids)]))
  }
  return typeof value === 'string' && value.startsWith('/media/') ? mediaID(ids, value) : value
}

async function publishContent(feature, data) {
  let entries = await listAll(`/api/v1/admin/features/${encodeURIComponent(feature.id)}/entries`)
  if (entries.length === 0) {
    const result = await request(`/api/v2/admin/features/${encodeURIComponent(feature.id)}/entries/import`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ data }),
    })
    entries = result.items ?? []
  }
  for (const entry of entries) {
    if (entry.dev_version_id) continue
    if (!entry.draft_version_id) {
      process.stdout.write(`content ${feature.key}/${entry.id} skipped; it has no draft to publish\n`)
      continue
    }
    const published = await request(`/api/v1/admin/entries/${encodeURIComponent(entry.id)}/publish`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ revision: entry.revision }),
    })
    process.stdout.write(`content ${feature.key}/${published.id} published to dev\n`)
  }
}

function literal(value) {
  return { source: 'literal', value }
}

function placement(build, slot, argumentsMap = {}, extra = {}) {
  return {
    id: `${build.key}-${slot}-${Math.random().toString(36).slice(2, 8)}`,
    build: { featureKey: build.key, version: build.version },
    slot,
    arguments: argumentsMap,
    ...extra,
  }
}

async function main() {
  const [legacySite, home, catalog, form, messages] = await Promise.all([
    readLegacy('site.json'),
    readLegacy('home.json'),
    readLegacy('catalog.json'),
    readLegacy('form.json'),
    readLegacy('messages.json'),
  ])
  await uploadFeatureBuilds()
  const media = await uploadLegacyMedia()
  const builds = await request('/api/v2/admin/feature-builds')
  const byKey = new Map()
  for (const build of builds.items ?? []) if (!byKey.has(build.key)) byKey.set(build.key, build)
  const required = ['background', 'header', 'hero', 'main-data', 'catalog', 'catalog-detail', 'form', 'footer', 'not-found']
  for (const key of required) if (!byKey.has(key)) throw new Error(`Feature build ${key} was not registered.`)

  const allFeatures = await listAll('/api/v1/admin/features?status=active')
  const features = new Map(allFeatures.map((feature) => [feature.key, feature]))
  const featureData = [
    ['header', replaceMediaRefs(legacySite.header, media)],
    ['hero', replaceMediaRefs(home.hero, media)],
    ['main-data', replaceMediaRefs(home.mainData, media)],
    ['footer', legacySite.footer],
    ['not-found', {
      title: messages.notFoundTitle,
      description: messages.notFoundDescription,
      backToHomeLabel: messages.backToHomeLabel,
      backToHomeTo: '/',
    }],
  ]
  for (const [key, data] of featureData) {
    const feature = features.get(key)
    if (!feature) throw new Error(`Manifest import did not register feature ${key}.`)
    await publishContent(feature, data)
  }

  const catalogFeature = features.get('catalog')
  const catalogItems = (catalog.content.items ?? []).map((item) => ({
    slug: String(item.id),
    title: item.title,
    price: item.price,
    ...(item.imageSrc ? { imageSrc: mediaID(media, item.imageSrc) } : {}),
    ...(item.description ? { description: item.description } : {}),
  }))
  await publishContent(catalogFeature, catalogItems)

  const homeBackground = mediaID(media, home.background.imageSrc)
  const catalogBackground = mediaID(media, catalog.background.imageSrc)
  const headerPlacement = () => placement(byKey.get('header'), 'header')
  const footerPlacement = () => placement(byKey.get('footer'), 'footer')
  const backgroundPlacement = (imageSrc) => placement(byKey.get('background'), 'background', { imageSrc: literal(imageSrc) })
  const pages = [
    {
      id: 'home', name: 'Главная', kind: 'page',
      placements: [backgroundPlacement(homeBackground), headerPlacement(), placement(byKey.get('hero'), 'main'), placement(byKey.get('main-data'), 'main'), footerPlacement()],
    },
    {
      id: 'catalog', name: 'Каталог', kind: 'collection-list',
      placements: [backgroundPlacement(catalogBackground), headerPlacement(), placement(byKey.get('catalog'), 'main', {
        title: literal(catalog.content.title),
        breadcrumb: literal(catalog.content.breadcrumb),
        action: literal(catalog.content.action),
      }, { detailLink: { routeId: 'product-detail', field: 'slug', pathTemplate: '/catalog/{slug}' } }), footerPlacement()],
    },
    {
      id: 'product-detail', name: 'Карточка товара', kind: 'detail',
      placements: [backgroundPlacement(catalogBackground), headerPlacement(), placement(byKey.get('catalog-detail'), 'main', {
        slug: { source: 'route', routeArg: 'slug' },
      }, { collectionSource: { featureKey: 'catalog', version: byKey.get('catalog').version, field: 'slug', argument: 'slug' } }), footerPlacement()],
    },
    {
      id: 'form', name: 'Форма заявки', kind: 'page',
      placements: [backgroundPlacement(homeBackground), headerPlacement(), placement(byKey.get('form'), 'main', Object.fromEntries(Object.entries(form).map(([key, value]) => [key, literal(value)]))), footerPlacement()],
    },
    {
      id: 'not-found', name: 'Страница не найдена', kind: 'page',
      placements: [backgroundPlacement(homeBackground), headerPlacement(), placement(byKey.get('not-found'), 'main'), footerPlacement()],
    },
  ]
  const config = {
    formatVersion: 1,
    documentTitle: legacySite.documentTitle,
    pages,
    routes: [
      { id: 'home', pattern: '^/$', pageId: 'home' },
      { id: 'catalog', pattern: '^/catalog$', pageId: 'catalog' },
      { id: 'product-detail', pattern: '^/catalog/(?<slug>[^/]+)$', pageId: 'product-detail' },
      { id: 'form', pattern: '^/form$', pageId: 'form' },
      { id: 'not-found', pattern: '^/.*$', pageId: 'not-found' },
    ],
  }

  const draft = await request('/api/v2/admin/site/draft')
  const saved = await request('/api/v2/admin/site/draft', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ revision: draft.revision, config }),
  })
  const snapshot = await request('/api/v2/admin/site/snapshots', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name: 'Перенос legacy сайта' }),
  })
  process.stdout.write(`site draft r${saved.revision} saved; dev snapshot #${snapshot.sequence} (${snapshot.id}) published\n`)
  process.stdout.write('The migration publishes to dev only. Promote this same snapshot to prod from /admin/site after review.\n')
}

await main()
