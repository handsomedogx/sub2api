import { apiClient } from '../client'

export async function listGroupTestModels(groupId: number, signal?: AbortSignal): Promise<string[]> {
  const { data } = await apiClient.get<{ models: string[] }>(`/admin/pelican-group-tests/models/${groupId}`, { signal })
  return data.models
}
