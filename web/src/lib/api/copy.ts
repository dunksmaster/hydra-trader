import type { CopyBotsResponse } from '../../types/copy'
import { API_BASE, httpClient } from './helpers'

export const copyApi = {
  async getCopyBots(): Promise<CopyBotsResponse> {
    const result = await httpClient.get<CopyBotsResponse>(`${API_BASE}/copy-bots`)
    if (!result.success || !result.data) {
      throw new Error(result.message || 'Failed to fetch copy bots')
    }
    return result.data
  },
}
