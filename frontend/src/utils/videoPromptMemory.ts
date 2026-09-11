/**
 * 提示词的本地记忆——**老任务的兜底**，不是主来源。
 *
 * 服务端现在会把提示词留在 `request_attributes.prompt` 里，`GET /v1/videos` 回放时
 * 原样带回（见 `VideoTask.prompt`），因此新任务根本不走这里。留着它只为一件事：
 * 服务端开始留存之前创建的那批任务，除了本浏览器这份记忆再无处可寻。
 *
 * 三条边界要清楚：
 *   · 只是缓存。清了站点数据、换了浏览器、或任务是从别处（API / 另一台设备）发起的，
 *     就取不到——此时展示"未保留"，不要编一段文案冒充原文。
 *   · 按任务 ID 取值，而任务 ID 本就属于某个用户，所以同一浏览器上换账号不会串读。
 *   · 有配额上限，写入必须容错；条数与单条长度都要封顶，否则一个长提示词刷几百次就能
 *     把 localStorage 顶满，连带影响同源下其它功能。
 */
const STORAGE_KEY = 'video-playground-prompts'
const MAX_ENTRIES = 200
const MAX_PROMPT_LENGTH = 2000

type PromptMemory = Record<string, string>

function read(): PromptMemory {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return {}
    const parsed = JSON.parse(raw)
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed)
      ? (parsed as PromptMemory)
      : {}
  } catch {
    return {}
  }
}

function write(memory: PromptMemory): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(memory))
  } catch {
    // 配额满或隐私模式禁写：提示词回放本就是锦上添花，静默放弃即可。
  }
}

export function rememberVideoPrompt(taskId: string, prompt: string): void {
  const id = taskId.trim()
  const text = prompt.trim()
  if (!id || !text) return
  const memory = read()
  // 先删后写：任务 ID 不是整数样式的键，对象的插入顺序因此就是写入顺序，
  // 淘汰最旧的一条只要砍最前面那个键。
  delete memory[id]
  memory[id] = text.slice(0, MAX_PROMPT_LENGTH)
  const keys = Object.keys(memory)
  for (const key of keys.slice(0, Math.max(0, keys.length - MAX_ENTRIES))) {
    delete memory[key]
  }
  write(memory)
}

export function recallVideoPrompt(taskId: string): string {
  const id = taskId.trim()
  if (!id) return ''
  return read()[id] || ''
}
