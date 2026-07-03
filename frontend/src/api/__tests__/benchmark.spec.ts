import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post, put, del } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  del: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    post,
    put,
    delete: del,
  },
}))

import { radarAPI } from '@/api/radar'
import { adminBenchmarkAPI } from '@/api/admin/benchmark'

describe('benchmark api', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    put.mockReset()
    del.mockReset()
  })

  it('calls public radar endpoint and returns targets and trends', async () => {
    const response = {
      ranking_basis: 'ability_score_only' as const,
      latest_run: null,
      targets: [
        {
          rank: 1,
          model: 'gpt-4.1',
          channel_id: 7,
          display_name: 'GPT-4.1',
          overall_score: 91.2,
          passed_count: 9,
          total_count: 10,
          metrics: { total_cost: 0.12 },
        },
      ],
      trends: [],
    }
    get.mockResolvedValue({ data: response })

    const result = await radarAPI.getCurrent()

    expect(get).toHaveBeenCalledWith('/public/radar')
    expect(result.targets).toEqual(response.targets)
    expect(result.trends).toEqual([])
  })

  it('lists admin benchmark targets with params', async () => {
    const response = { items: [], total: 0, page: 2, page_size: 50, pages: 1 }
    get.mockResolvedValue({ data: response })

    const result = await adminBenchmarkAPI.listTargets({ page: 2, page_size: 50 })

    expect(get).toHaveBeenCalledWith('/admin/benchmark/targets', {
      params: { page: 2, page_size: 50 },
    })
    expect(result).toEqual(response)
  })

  it('creates a benchmark target and defaults enabled to true', async () => {
    const payload = { model_name: 'gpt-4.1', channel_id: 7, display_name: 'GPT-4.1' }
    post.mockResolvedValue({ data: payload })

    await adminBenchmarkAPI.createTarget(payload)

    expect(post).toHaveBeenCalledWith('/admin/benchmark/targets', { ...payload, enabled: true })
  })

  it('preserves explicit disabled state when creating a target', async () => {
    const payload = { model_name: 'gpt-4.1', channel_id: 7, enabled: false }
    post.mockResolvedValue({ data: payload })

    await adminBenchmarkAPI.createTarget(payload)

    expect(post).toHaveBeenCalledWith('/admin/benchmark/targets', payload)
  })

  it('creates a benchmark task and defaults enabled to true', async () => {
    const payload = {
      title: 'Reasoning sample',
      type: 'reasoning',
      prompt: 'Solve it',
      verifier_type: 'exact_match',
    }
    post.mockResolvedValue({ data: { id: 2, ...payload } })

    await adminBenchmarkAPI.createTask(payload)

    expect(post).toHaveBeenCalledWith('/admin/benchmark/tasks', { ...payload, enabled: true })
  })

  it('serializes task type array filters as comma strings', async () => {
    const response = { items: [], total: 0, page: 1, page_size: 20, pages: 1 }
    get.mockResolvedValue({ data: response })

    await adminBenchmarkAPI.listTasks({ page: 1, page_size: 20, task_types: ['reasoning', 'coding'] })

    expect(get).toHaveBeenCalledWith('/admin/benchmark/tasks', {
      params: { page: 1, page_size: 20, task_types: 'reasoning,coding' },
    })
  })

  it('previews a run by target ids and task count', async () => {
    const payload = { target_ids: [7, 8], task_count: 5 }
    const response = {
      target_count: 2,
      task_count: 5,
      result_count: 10,
      ranking_basis: 'ability_score_only',
      target_ids: [7, 8],
      task_ids: [1, 2, 3, 4, 5],
    }
    post.mockResolvedValue({ data: response })

    const result = await adminBenchmarkAPI.previewRun(payload)

    expect(post).toHaveBeenCalledWith('/admin/benchmark/runs/preview', payload)
    expect(result).toEqual(response)
  })

  it('creates and publishes runs', async () => {
    const payload = { target_ids: [7], task_count: 10, trigger_type: 'manual', process_immediately: true }
    const run = {
      id: 9,
      status: 'queued',
      trigger_type: 'manual',
      task_count: 10,
      planned_target_count: 1,
      planned_task_count: 10,
      planned_result_count: 10,
    }
    post
      .mockResolvedValueOnce({ data: run })
      .mockResolvedValueOnce({ data: { message: 'ok' } })

    const created = await adminBenchmarkAPI.createRun(payload)
    const published = await adminBenchmarkAPI.publishRun(9)

    expect(post).toHaveBeenNthCalledWith(1, '/admin/benchmark/runs', payload)
    expect(post).toHaveBeenNthCalledWith(2, '/admin/benchmark/runs/9/publish')
    expect(created).toEqual(run)
    expect(published).toEqual({ message: 'ok' })
  })

  it('updates and deletes targets and schedules by id', async () => {
    const targetPayload = {
      model_name: 'gpt-4.1',
      channel_id: 7,
      display_name: 'GPT-4.1',
      enabled: false,
      public_visible: true,
      sort_order: 10,
    }
    const schedulePayload = {
      name: 'Updated schedule',
      cron_expr: '0 4 * * *',
      enabled: true,
      target_ids: [7],
      task_count: 5,
    }
    put
      .mockResolvedValueOnce({ data: { id: 11, ...targetPayload } })
      .mockResolvedValueOnce({ data: { id: 8, ...schedulePayload } })
    del
      .mockResolvedValueOnce({ data: { message: 'target deleted' } })
      .mockResolvedValueOnce({ data: { message: 'task deleted' } })
      .mockResolvedValueOnce({ data: { message: 'schedule deleted' } })

    const updatedTarget = await adminBenchmarkAPI.updateTarget(11, targetPayload)
    await adminBenchmarkAPI.deleteTarget(11)
    await adminBenchmarkAPI.deleteTask(21)
    const updatedSchedule = await adminBenchmarkAPI.updateSchedule(8, schedulePayload)
    await adminBenchmarkAPI.deleteSchedule(8)

    expect(put).toHaveBeenNthCalledWith(1, '/admin/benchmark/targets/11', targetPayload)
    expect(del).toHaveBeenNthCalledWith(1, '/admin/benchmark/targets/11')
    expect(del).toHaveBeenNthCalledWith(2, '/admin/benchmark/tasks/21')
    expect(put).toHaveBeenNthCalledWith(2, '/admin/benchmark/schedules/8', schedulePayload)
    expect(del).toHaveBeenNthCalledWith(3, '/admin/benchmark/schedules/8')
    expect(updatedTarget.enabled).toBe(false)
    expect(updatedSchedule.name).toBe('Updated schedule')
  })

  it('runs cancel and processing actions on benchmark runs', async () => {
    post
      .mockResolvedValueOnce({ data: { message: 'canceled' } })
      .mockResolvedValueOnce({ data: { processed: 1 } })
      .mockResolvedValueOnce({ data: { processed: 3 } })

    await adminBenchmarkAPI.cancelRun(31)
    await adminBenchmarkAPI.processRun(31)
    const processedDue = await adminBenchmarkAPI.processDueRuns()

    expect(post).toHaveBeenNthCalledWith(1, '/admin/benchmark/runs/31/cancel')
    expect(post).toHaveBeenNthCalledWith(2, '/admin/benchmark/runs/31/process')
    expect(post).toHaveBeenNthCalledWith(3, '/admin/benchmark/runs/process-due')
    expect(processedDue).toEqual({ processed: 3 })
  })

  it('lists, creates, and triggers schedules with target ids and task count', async () => {
    const listResponse = { items: [], total: 0, page: 1, page_size: 10, pages: 1 }
    const payload = {
      name: 'Nightly radar',
      cron_expr: '0 3 * * *',
      target_ids: [7],
      task_count: 12,
    }
    get.mockResolvedValue({ data: listResponse })
    post
      .mockResolvedValueOnce({ data: { id: 8, ...payload, enabled: false } })
      .mockResolvedValueOnce({ data: { id: 13, status: 'queued', trigger_type: 'scheduled' } })

    await adminBenchmarkAPI.listSchedules({ enabled: true, page: 1, page_size: 10 })
    await adminBenchmarkAPI.createSchedule(payload)
    const triggered = await adminBenchmarkAPI.triggerSchedule(8)

    expect(get).toHaveBeenCalledWith('/admin/benchmark/schedules', {
      params: { enabled: true, page: 1, page_size: 10 },
    })
    expect(post).toHaveBeenNthCalledWith(1, '/admin/benchmark/schedules', payload)
    expect(post).toHaveBeenNthCalledWith(2, '/admin/benchmark/schedules/8/trigger')
    expect(triggered.trigger_type).toBe('scheduled')
  })

  it('fetches trends with day/limit params', async () => {
    get.mockResolvedValue({ data: { trends: [] } })

    await adminBenchmarkAPI.getTrends({ days: 30, limit: 500 })

    expect(get).toHaveBeenCalledWith('/admin/benchmark/trends', { params: { days: 30, limit: 500 } })
  })

  it('serializes run status array filters as comma strings', async () => {
    const response = { items: [], total: 0, page: 1, page_size: 20, pages: 1 }
    get.mockResolvedValue({ data: response })

    await adminBenchmarkAPI.listRuns({ page: 1, page_size: 20, status: ['queued', 'running'] })

    expect(get).toHaveBeenCalledWith('/admin/benchmark/runs', {
      params: { page: 1, page_size: 20, status: 'queued,running' },
    })
  })
})
