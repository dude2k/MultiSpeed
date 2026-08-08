import { spawnSync } from 'node:child_process'

export default function removeDockerTestBackend(): void {
  const docker = process.env.MULTISPEED_E2E_DOCKER_COMMAND?.trim() || 'docker'
  const container = process.env.MULTISPEED_E2E_CONTAINER_NAME?.trim() || 'multispeed-e2e-playwright'
  if (!/^multispeed-e2e-[a-zA-Z0-9_.-]+$/.test(container)) return
  const inspect = spawnSync(docker, ['inspect', '--format', '{{index .Config.Labels "io.multispeed.e2e"}}', container], { encoding: 'utf8' })
  if (inspect.status === 0 && inspect.stdout.trim() === 'true') {
    spawnSync(docker, ['rm', '--force', container], { stdio: 'ignore' })
  }
}
