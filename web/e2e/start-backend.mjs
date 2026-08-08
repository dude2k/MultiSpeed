import { chmodSync, mkdtempSync, mkdirSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, basename, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawn, spawnSync } from 'node:child_process'

const scriptDirectory = dirname(fileURLToPath(import.meta.url))
const webDirectory = resolve(scriptDirectory, '..')
const repositoryRoot = resolve(webDirectory, '..')
const fixtureDirectory = join(scriptDirectory, 'fixtures')
const dataDirectory = mkdtempSync(join(tmpdir(), 'multispeed-e2e-'))
const requestedPort = process.env.MULTISPEED_E2E_BACKEND_PORT ?? '18787'
const port = Number(requestedPort)

if (!Number.isInteger(port) || port < 1024 || port > 65535) {
  throw new Error(`MULTISPEED_E2E_BACKEND_PORT must be an integer from 1024 to 65535; received ${requestedPort}`)
}

const ooklaFixture = join(fixtureDirectory, 'ookla-speedtest')
const libreSpeedFixture = join(fixtureDirectory, 'librespeed-cli')
for (const fixture of [ooklaFixture, libreSpeedFixture]) {
  try {
    chmodSync(fixture, 0o755)
  } catch (error) {
    if (process.platform !== 'win32') throw error
  }
}

const backendEnvironment = {
  ...process.env,
  APP_LISTEN_ADDR: `127.0.0.1:${port}`,
  APP_DATA_DIR: dataDirectory,
  APP_LOG_LEVEL: 'INFO',
  APP_SHUTDOWN_TIMEOUT: '5s',
  OOKLA_BINARY: ooklaFixture,
  LIBRESPEED_BINARY: libreSpeedFixture,
  ACCEPT_OOKLA_EULA: 'false',
  NO_PROXY: appendNoProxy(process.env.NO_PROXY),
  no_proxy: appendNoProxy(process.env.no_proxy),
}

let activeChild
let dockerContainer = ''
let dockerCommand = ''
let shutdownRequested = false

for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => {
    shutdownRequested = true
    activeChild?.kill(signal)
    removeDockerContainer()
  })
}

try {
  const customCommand = process.env.MULTISPEED_E2E_BACKEND_COMMAND?.trim()
  if (customCommand) {
    process.stdout.write(`[multispeed-e2e] starting backend override: ${customCommand}\n`)
    activeChild = spawn(customCommand, { cwd: repositoryRoot, env: backendEnvironment, shell: true, stdio: 'inherit' })
  } else {
    const goCommand = process.env.MULTISPEED_E2E_GO_COMMAND?.trim() || 'go'
    if (process.platform === 'linux' && commandSucceeds(goCommand, ['version'])) {
      const binaryDirectory = join(dataDirectory, 'bin')
      mkdirSync(binaryDirectory, { recursive: true })
      const binaryPath = join(binaryDirectory, 'multispeed-e2e')
      process.stdout.write(`[multispeed-e2e] building backend with ${goCommand}\n`)
      await runToCompletion(goCommand, ['build', '-trimpath', '-o', binaryPath, './web/e2e/backend'], backendEnvironment)
      activeChild = spawn(binaryPath, { cwd: repositoryRoot, env: backendEnvironment, stdio: 'inherit' })
    } else {
      dockerCommand = process.env.MULTISPEED_E2E_DOCKER_COMMAND?.trim() || 'docker'
      if (!commandSucceeds(dockerCommand, ['version', '--format', '{{.Server.Version}}'])) {
        throw new Error('A Linux Go toolchain or a running Docker daemon is required. Set MULTISPEED_E2E_BACKEND_COMMAND or MULTISPEED_E2E_GO_COMMAND to override discovery.')
      }
      const configuredImage = process.env.MULTISPEED_E2E_DOCKER_IMAGE?.trim()
      const image = configuredImage || 'multispeed-e2e:local'
      if (!configuredImage) {
        process.stdout.write('[multispeed-e2e] no Linux Go toolchain found; building the Docker test backend\n')
        await runToCompletion(dockerCommand, ['build', '--file', 'web/e2e/Dockerfile', '--tag', image, '.'], process.env)
      }
      dockerContainer = process.env.MULTISPEED_E2E_CONTAINER_NAME?.trim() || 'multispeed-e2e-playwright'
      if (!/^multispeed-e2e-[a-zA-Z0-9_.-]+$/.test(dockerContainer)) {
        throw new Error('MULTISPEED_E2E_CONTAINER_NAME must start with multispeed-e2e- and contain only safe name characters.')
      }
      removeDockerContainer()
      dockerContainer = process.env.MULTISPEED_E2E_CONTAINER_NAME?.trim() || 'multispeed-e2e-playwright'
      activeChild = spawn(dockerCommand, [
        'run', '--rm', '--name', dockerContainer,
        '--label', 'io.multispeed.e2e=true',
        '--publish', `127.0.0.1:${port}:${port}`,
        '--env', `APP_LISTEN_ADDR=0.0.0.0:${port}`,
        '--env', 'APP_DATA_DIR=/data',
        image,
      ], { cwd: repositoryRoot, env: process.env, stdio: 'inherit' })
    }
  }

  const exit = await childExit(activeChild)
  const removedByPlaywrightTeardown = Boolean(dockerCommand) && exit.code === 137
  if (!shutdownRequested && !removedByPlaywrightTeardown && exit.code !== 0) {
    throw new Error(`e2e backend exited unexpectedly (${describeExit(exit)})`)
  }
} finally {
  removeDockerContainer()
  const temporaryRoot = resolve(tmpdir())
  const resolvedDataDirectory = resolve(dataDirectory)
  if (dirname(resolvedDataDirectory) === temporaryRoot && basename(resolvedDataDirectory).startsWith('multispeed-e2e-')) {
    rmSync(resolvedDataDirectory, { recursive: true, force: true })
  }
}

function commandSucceeds(command, args) {
  const result = spawnSync(command, args, { cwd: repositoryRoot, env: process.env, stdio: 'ignore' })
  return !result.error && result.status === 0
}

function runToCompletion(command, args, environment) {
  const child = spawn(command, args, { cwd: repositoryRoot, env: environment, stdio: 'inherit' })
  activeChild = child
  return childExit(child).then((exit) => {
    if (exit.code !== 0) throw new Error(`${command} ${args[0] ?? ''} failed (${describeExit(exit)})`)
  })
}

function childExit(child) {
  return new Promise((resolveExit, reject) => {
    child.once('error', reject)
    child.once('exit', (code, signal) => resolveExit({ code, signal }))
  })
}

function removeDockerContainer() {
  if (!dockerContainer || !dockerCommand) return
  const inspect = spawnSync(dockerCommand, ['inspect', '--format', '{{index .Config.Labels "io.multispeed.e2e"}}', dockerContainer], { cwd: repositoryRoot, env: process.env, encoding: 'utf8' })
  if (inspect.status === 0 && inspect.stdout.trim() === 'true') {
    spawnSync(dockerCommand, ['rm', '--force', dockerContainer], { cwd: repositoryRoot, env: process.env, stdio: 'ignore' })
  }
  dockerContainer = ''
}

function appendNoProxy(current) {
  const values = new Set((current ?? '').split(',').map((value) => value.trim()).filter(Boolean))
  values.add('127.0.0.1')
  values.add('localhost')
  return [...values].join(',')
}

function describeExit(exit) {
  return exit.signal ? `signal ${exit.signal}` : `exit code ${exit.code ?? 'unknown'}`
}
