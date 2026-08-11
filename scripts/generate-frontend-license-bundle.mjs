#!/usr/bin/env node

import { createHash } from 'node:crypto'
import { copyFile, mkdir, readFile, readdir, stat, writeFile } from 'node:fs/promises'
import path from 'node:path'
import process from 'node:process'

const [lockfileArgument, modulesArgument, outputArgument, overridesArgument] = process.argv.slice(2)
if (!lockfileArgument || !modulesArgument || !outputArgument) {
  throw new Error(
    'usage: generate-frontend-license-bundle.mjs <package-lock.json> <node_modules> <output> [license-overrides]',
  )
}

const lockfilePath = path.resolve(lockfileArgument)
const modulesPath = path.resolve(modulesArgument)
const outputPath = path.resolve(outputArgument)
const overridesPath = overridesArgument ? path.resolve(overridesArgument) : undefined
const lockfile = JSON.parse(await readFile(lockfilePath, 'utf8'))
const outputContributingBuildTools = new Set(['rolldown', 'tailwindcss', 'vite'])
const packageEntries = Object.entries(lockfile.packages ?? {})
  .filter(([key, value]) => {
    if (!key.startsWith('node_modules/')) return false
    const name = value.name ?? packageNameFromLockPath(key)
    return value.dev !== true || outputContributingBuildTools.has(name)
  })
  .sort(([left], [right]) => left.localeCompare(right))

const licenseName = /^(?:(?:licen[cs]e|copying|notice|copyright)(?:[._-].*)?|third[._-]party[._-]licen[cs]es?)$/i
const manifest = []

for (const [lockPath, metadata] of packageEntries) {
  const relativeModulePath = lockPath.slice('node_modules/'.length)
  const installedPath = path.join(modulesPath, ...relativeModulePath.split('/'))
  if (!(await exists(installedPath))) {
    if (metadata.optional === true) continue
    throw new Error(`included frontend component is not installed: ${relativeModulePath}`)
  }

  const packageMetadata = JSON.parse(await readFile(path.join(installedPath, 'package.json'), 'utf8'))
  const packageName = packageMetadata.name ?? packageNameFromLockPath(lockPath)
  const version = packageMetadata.version ?? metadata.version
  const declaredLicense = packageMetadata.license ?? metadata.license ?? 'UNKNOWN'
  const destinationName = `${safeName(packageName)}@${version}`
  const destinationRoot = path.join(outputPath, destinationName)
  let sourceRoot = installedPath
  let noticeSource = 'package'
  let sourceFiles = await findLicenseFiles(sourceRoot)
  if (sourceFiles.length === 0 && overridesPath) {
    const overrideRoot = path.join(overridesPath, destinationName)
    if (await exists(overrideRoot)) {
      sourceRoot = overrideRoot
      noticeSource = 'tracked-override'
      sourceFiles = await findLicenseFiles(sourceRoot)
    }
  }
  if (sourceFiles.length === 0) {
    throw new Error(`production dependency has no distributable license or notice file: ${packageName}@${version}`)
  }

  const files = []
  for (const sourceFile of sourceFiles) {
    const relativePath = path.relative(sourceRoot, sourceFile)
    const destination = path.join(destinationRoot, relativePath)
    const data = await readFile(sourceFile)
    await mkdir(path.dirname(destination), { recursive: true })
    await copyFile(sourceFile, destination)
    files.push({ path: relativePath.replaceAll('\\', '/'), sha256: sha256(data) })
  }
  const packageData = Buffer.from(`${JSON.stringify({ name: packageName, version, license: declaredLicense }, null, 2)}\n`, 'utf8')
  await writeFile(path.join(destinationRoot, 'package.json'), packageData)
  files.push({ path: 'package.json', sha256: sha256(packageData) })
  manifest.push({ name: packageName, version, declaredLicense, noticeSource, files })
}

await mkdir(outputPath, { recursive: true })
await writeFile(
  path.join(outputPath, 'manifest.json'),
  `${JSON.stringify({ format: 'multispeed-npm-license-bundle-v1', packages: manifest }, null, 2)}\n`,
  'utf8',
)

for (const name of outputContributingBuildTools) {
  if (!manifest.some((entry) => entry.name === name)) {
    throw new Error(`output-contributing build tool is missing from the license manifest: ${name}`)
  }
}
const rolldown = manifest.find((entry) => entry.name === 'rolldown')
if (!rolldown.files.some((file) => /^third[._-]party[._-]license$/i.test(path.basename(file.path)))) {
  throw new Error('Rolldown THIRD-PARTY-LICENSE is missing from the license bundle')
}

async function findLicenseFiles(root) {
  const matches = []
  await walk(root, '')
  return matches.sort((left, right) => left.localeCompare(right))

  async function walk(current, relative) {
    const entries = await readdir(current, { withFileTypes: true })
    entries.sort((left, right) => left.name.localeCompare(right.name))
    for (const entry of entries) {
      if (entry.name === 'node_modules' || entry.name === '.git') continue
      const child = path.join(current, entry.name)
      const childRelative = path.join(relative, entry.name)
      if (entry.isDirectory()) {
        await walk(child, childRelative)
      } else if (entry.isFile() && licenseName.test(entry.name)) {
        matches.push(child)
      }
    }
  }
}

async function exists(target) {
  try {
    return (await stat(target)).isDirectory()
  } catch (error) {
    if (error?.code === 'ENOENT') return false
    throw error
  }
}

function packageNameFromLockPath(lockPath) {
  const parts = lockPath.split('node_modules/').at(-1).split('/')
  return parts[0].startsWith('@') ? `${parts[0]}/${parts[1]}` : parts[0]
}

function safeName(value) {
  return value.replace(/^@/, '').replaceAll('/', '__').replaceAll('\\', '__')
}

function sha256(data) {
  return createHash('sha256').update(data).digest('hex')
}
