import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    post,
  },
}))

import { radarAPI } from '@/api/radar'
import { adminBenchmarkAPI } from '@/api/admin/benchmark'

describe('benchmark api', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  it('calls public radar endpoint and returns targets', async () => {
    const response = {
      ranking_basis: 'ability_score_only' as const,
      latest_run: null,
      targets: [
        {
          rank: 1,
          model: 'gpt-4.1',
          channel_id: 7,
          display_name: 'GPT-4.1',
          overall_score: 0.91,
          score_basis: {
            planned_tasks: 10,
            scored_tasks: 9,
            invalid_tasks: 1,
            coverage_rate: 0.9,
            confidence_level: 'high',
            insufficient_sample: false,
          },
          metrics: {
            success_rate: 0.9,
            estimated_cost: 0.12,
          },
        },
      ],
    }
    get.mockResolvedValue({ data: response })

    const result = await radarAPI.getCurrent()

    expect(get).toHaveBeenCalledWith('/public/radar')
    expect(result.targets).toEqual(response.targets)
  })

  it('lists admin benchmark targets with params', async () => {
    const response = {
      items: [],
      total: 0,
      page: 2,
      page_size: 50,
      pages: 1,
    }
    get.mockResolvedValue({ data: response })

    const result = await adminBenchmarkAPI.listTargets({
      page: 2,
      page_size: 50,
    })

    expect(get).toHaveBeenCalledWith('/admin/benchmark/targets', {
      params: {
        page: 2,
        page_size: 50,
      },
    })
    expect(result).toEqual(response)
  })

  it('creates admin benchmark target with model and channel identifiers', async () => {
    const payload = {
      model_name: 'gpt-4.1',
      channel_id: 7,
      display_name: 'GPT-4.1',
      supported_task_types: ['reasoning'],
    }
    get.mockResolvedValue({ data: {} })
    post.mockResolvedValue({ data: payload })

    const result = await adminBenchmarkAPI.createTarget(payload)

    expect(post).toHaveBeenCalledWith('/admin/benchmark/targets', {
      ...payload,
      enabled: true,
    })
    expect(result).toEqual(payload)
  })

  it('defaults enabled resources to enabled when creating them', async () => {
    const suitePayload = {
      name: 'Core Radar',
      slug: 'core-radar',
    }
    const taskPayload = {
      suite_id: 11,
      title: 'Reasoning sample',
      type: 'reasoning',
      prompt: 'Solve it',
      verifier_type: 'exact_match',
    }
    const profilePayload = {
      suite_id: 11,
      name: 'Default Profile',
      target_ids: [1],
      task_types: ['reasoning'],
    }
    post
      .mockResolvedValueOnce({ data: { id: 1, ...suitePayload } })
      .mockResolvedValueOnce({ data: { id: 2, ...taskPayload } })
      .mockResolvedValueOnce({ data: { id: 3, ...profilePayload } })

    await adminBenchmarkAPI.createSuite(suitePayload)
    await adminBenchmarkAPI.createTask(taskPayload)
    await adminBenchmarkAPI.createProfile(profilePayload)

    expect(post).toHaveBeenNthCalledWith(1, '/admin/benchmark/suites', {
      ...suitePayload,
      enabled: true,
    })
    expect(post).toHaveBeenNthCalledWith(2, '/admin/benchmark/tasks', {
      ...taskPayload,
      enabled: true,
    })
    expect(post).toHaveBeenNthCalledWith(3, '/admin/benchmark/profiles', {
      ...profilePayload,
      enabled: true,
    })
  })

  it('preserves explicit disabled state when creating enabled resources', async () => {
    const payload = {
      model_name: 'gpt-4.1',
      channel_id: 7,
      enabled: false,
    }
    post.mockResolvedValue({ data: payload })

    await adminBenchmarkAPI.createTarget(payload)

    expect(post).toHaveBeenCalledWith('/admin/benchmark/targets', payload)
  })

  it('serializes task type array filters as comma strings', async () => {
    const response = {
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
      pages: 1,
    }
    get.mockResolvedValue({ data: response })

    const result = await adminBenchmarkAPI.listTasks({
      page: 1,
      page_size: 20,
      task_types: ['reasoning', 'coding'],
    })

    expect(get).toHaveBeenCalledWith('/admin/benchmark/tasks', {
      params: {
        page: 1,
        page_size: 20,
        task_types: 'reasoning,coding',
      },
    })
    expect(result).toEqual(response)
  })

  it('lists and creates profiles', async () => {
    const listResponse = {
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
      pages: 1,
    }
    const payload = {
      suite_id: 11,
      name: 'Default Profile',
      target_ids: [1, 2],
      task_types: ['reasoning', 'coding'],
      task_scale: 'medium' as const,
    }
    get.mockResolvedValue({ data: listResponse })
    post.mockResolvedValue({ data: { id: 3, ...payload } })

    const listed = await adminBenchmarkAPI.listProfiles({ page: 1, page_size: 20 })
    const created = await adminBenchmarkAPI.createProfile(payload)

    expect(get).toHaveBeenCalledWith('/admin/benchmark/profiles', {
      params: { page: 1, page_size: 20 },
    })
    expect(post).toHaveBeenCalledWith('/admin/benchmark/profiles', {
      ...payload,
      enabled: true,
    })
    expect(listed).toEqual(listResponse)
    expect(created).toEqual({ id: 3, ...payload })
  })

  it('previews a benchmark profile by id', async () => {
    const payload = {
      task_types: ['reasoning'],
      task_scale: 'small' as const,
      task_count_limit: 5,
    }
    const response = {
      target_count: 2,
      task_count: 5,
      result_count: 10,
      task_types: ['reasoning'],
      task_scale: 'small',
      ranking_basis: 'ability_score_only',
      estimated_cost: 0,
      selected_task_ids: [1, 2, 3, 4, 5],
      selected_target_ids: [7, 8],
    }
    post.mockResolvedValue({ data: response })

    const result = await adminBenchmarkAPI.previewProfile(5, payload)

    expect(post).toHaveBeenCalledWith('/admin/benchmark/profiles/5/preview', payload)
    expect(result).toEqual(response)
  })

  it('creates and publishes runs', async () => {
    const payload = {
      profile_id: 5,
      trigger_type: 'manual',
      override: {
        task_types: ['reasoning'],
      },
    }
    const run = {
      id: 9,
      suite_id: 11,
      profile_id: 5,
      status: 'queued',
      trigger_type: 'manual',
      task_scale: 'medium',
      task_types: ['reasoning'],
      planned_target_count: 1,
      planned_task_count: 2,
      planned_result_count: 2,
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

  it('lists, creates, and triggers schedules', async () => {
    const listResponse = {
      items: [],
      total: 0,
      page: 1,
      page_size: 10,
      pages: 1,
    }
    const payload = {
      profile_id: 5,
      name: 'Nightly radar',
      cron_expr: '0 3 * * *',
      enabled: true,
      metadata: {
        scope: 'nightly',
      },
    }
    get.mockResolvedValue({ data: listResponse })
    post
      .mockResolvedValueOnce({ data: { id: 8, ...payload } })
      .mockResolvedValueOnce({
        data: {
          id: 13,
          suite_id: 11,
          profile_id: 5,
          status: 'queued',
          trigger_type: 'scheduled',
          task_scale: 'medium',
          task_types: ['reasoning'],
          planned_target_count: 1,
          planned_task_count: 1,
          planned_result_count: 1,
        },
      })

    const listed = await adminBenchmarkAPI.listSchedules({
      profile_id: 5,
      enabled: true,
      page: 1,
      page_size: 10,
    })
    const created = await adminBenchmarkAPI.createSchedule(payload)
    const triggered = await adminBenchmarkAPI.triggerSchedule(8)

    expect(get).toHaveBeenCalledWith('/admin/benchmark/schedules', {
      params: {
        profile_id: 5,
        enabled: true,
        page: 1,
        page_size: 10,
      },
    })
    expect(post).toHaveBeenNthCalledWith(1, '/admin/benchmark/schedules', payload)
    expect(post).toHaveBeenNthCalledWith(2, '/admin/benchmark/schedules/8/trigger')
    expect(listed).toEqual(listResponse)
    expect(created).toEqual({ id: 8, ...payload })
    expect(triggered.id).toBe(13)
    expect(triggered.trigger_type).toBe('scheduled')
  })

  it('serializes run status array filters as comma strings', async () => {
    const response = {
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
      pages: 1,
    }
    get.mockResolvedValue({ data: response })

    const result = await adminBenchmarkAPI.listRuns({
      page: 1,
      page_size: 20,
      status: ['queued', 'running'],
    })

    expect(get).toHaveBeenCalledWith('/admin/benchmark/runs', {
      params: {
        page: 1,
        page_size: 20,
        status: 'queued,running',
      },
    })
    expect(result).toEqual(response)
  })
})
