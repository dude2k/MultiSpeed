import type { Task, TaskInput } from './types'

export function taskToInput(task: Task): TaskInput {
  return {
    name: task.name,
    description: task.description,
    enabled: task.enabled,
    provider: task.provider,
    cronExpression: task.cronExpression,
    timezone: task.timezone,
    randomJitterSeconds: task.randomJitterSeconds,
    serverSelectionMode: task.serverSelectionMode,
    serverId: task.serverId,
    serverUrl: task.serverUrl,
    customServerDefinition: task.customServerDefinition ?? {},
    interfaceName: task.interfaceName,
    sourceIp: task.sourceIp,
    ipFamily: task.ipFamily,
    routeProfileId: task.routeProfileId,
    timeoutSeconds: task.timeoutSeconds,
    providerOptions: task.providerOptions ?? {},
    preventOverlap: task.preventOverlap,
    routeValidation: task.routeValidation,
  }
}
