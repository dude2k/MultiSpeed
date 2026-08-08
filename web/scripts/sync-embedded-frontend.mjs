import { createHash } from 'node:crypto'
import { cp, mkdir, readdir, readFile, rm, stat } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const repositoryRoot = path.resolve(webRoot, '..')
const source = path.resolve(webRoot, 'dist')
const destination = path.resolve(repositoryRoot, 'internal', 'frontend', 'dist')
const expectedDestination = path.join(repositoryRoot, 'internal', 'frontend', 'dist')

if (destination !== expectedDestination || !destination.startsWith(`${repositoryRoot}${path.sep}`)) {
  throw new Error(`Refusing to synchronize an unexpected embedded frontend path: ${destination}`)
}
if (!(await stat(source).catch(() => null))?.isDirectory()) {
  throw new Error(`Vite output is missing: ${source}`)
}

if (process.argv.includes('--check')) {
  const differences = await compareTrees(source, destination)
  if (differences.length) throw new Error(`Embedded frontend is out of date:\n${differences.join('\n')}`)
  console.log('Embedded frontend matches web/dist.')
} else {
  await rm(destination, { recursive: true, force: true })
  await mkdir(path.dirname(destination), { recursive: true })
  await cp(source, destination, { recursive: true })
  console.log(`Synchronized ${path.relative(repositoryRoot, source)} -> ${path.relative(repositoryRoot, destination)}`)
}

async function compareTrees(leftRoot, rightRoot) {
  const [left, right] = await Promise.all([treeHashes(leftRoot), treeHashes(rightRoot)])
  const names = [...new Set([...left.keys(), ...right.keys()])].sort()
  return names.flatMap((name) => left.get(name) === right.get(name) ? [] : [`${name}: ${left.has(name) ? 'source changed' : 'source missing'}${right.has(name) ? '' : ', embedded file missing'}`])
}

async function treeHashes(root) {
  const result = new Map()
  const rootStats = await stat(root).catch(() => null)
  if (!rootStats?.isDirectory()) return result
  await visit(root, '')
  return result

  async function visit(directory, relativeDirectory) {
    const entries = await readdir(directory, { withFileTypes: true })
    for (const entry of entries) {
      const relative = path.posix.join(relativeDirectory.split(path.sep).join(path.posix.sep), entry.name)
      const absolute = path.join(directory, entry.name)
      if (entry.isDirectory()) await visit(absolute, relative)
      else if (entry.isFile()) result.set(relative, createHash('sha256').update(await readFile(absolute)).digest('hex'))
    }
  }
}
