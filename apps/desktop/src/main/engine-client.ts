import { spawn, type ChildProcessWithoutNullStreams } from 'node:child_process'
import { PROTOCOL_VERSION, type Hello } from '../../../../contracts/index.ts'

type Pending = {
  resolve(value: unknown): void
  reject(error: Error): void
  timer: ReturnType<typeof setTimeout>
}
const MAX_FRAME_BYTES = 1024 * 1024

export class EngineClient {
  private child?: ChildProcessWithoutNullStreams
  private pending = new Map<string, Pending>()
  private sequence = 0
  private buffer = ''
  private failure?: Error
  private stopping = false

  constructor(
    private executable: string,
    private timeoutMs = 5000,
    private args: string[] = []
  ) {}

  async start(): Promise<Hello> {
    if (this.child) throw new Error('引擎已经启动')
    const child = spawn(this.executable, this.args, { windowsHide: true, stdio: 'pipe' })
    this.child = child
    child.stdout.setEncoding('utf8')
    child.stdout.on('data', (chunk: string) => this.consume(chunk))
    child.stderr.setEncoding('utf8')
    child.stderr.on('data', (chunk: string) =>
      console.error('[engine]', chunk.slice(0, 2000).trim())
    )
    child.on('error', (error) => this.fail(new Error(`引擎无法启动：${error.message}`)))
    child.stdin.on('error', (error) => this.fail(new Error(`引擎输入已关闭：${error.message}`)))
    child.on('exit', (code, signal) =>
      this.fail(new Error(`引擎已退出（${signal ?? code ?? 'unknown'}），请重新连接`))
    )
    try {
      const hello = await this.request<Hello>('hello')
      if (
        hello.protocolVersion !== PROTOCOL_VERSION ||
        hello.recordVersion !== 1 ||
        !hello.capabilities?.includes('runs.list') ||
        !hello.capabilities.includes('runs.events')
      ) {
        throw new Error('引擎版本或能力不兼容')
      }
      return hello
    } catch (error) {
      await this.stop()
      throw error
    }
  }

  request<T>(method: string, params: unknown = {}, timeoutMs = this.timeoutMs): Promise<T> {
    if (this.failure) return Promise.reject(this.failure)
    if (!this.child || this.stopping) return Promise.reject(new Error('引擎未就绪'))
    if (this.pending.size >= 128) return Promise.reject(new Error('请求过多，请稍后重试'))
    const id = String(++this.sequence)
    const frame =
      JSON.stringify({ type: 'request', version: PROTOCOL_VERSION, id, method, params }) + '\n'
    if (Buffer.byteLength(frame) >= MAX_FRAME_BYTES)
      return Promise.reject(new Error('请求超过大小限制'))
    return new Promise<T>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id)
        reject(new Error(`引擎请求超时：${method}`))
      }, timeoutMs)
      this.pending.set(id, { resolve: (value) => resolve(value as T), reject, timer })
      this.child!.stdin.write(frame, (error) => {
        if (error) this.fail(error)
      })
    })
  }

  private consume(chunk: string): void {
    this.buffer += chunk
    let end: number
    while ((end = this.buffer.indexOf('\n')) !== -1) {
      const line = this.buffer.slice(0, end)
      this.buffer = this.buffer.slice(end + 1)
      if (Buffer.byteLength(line) >= MAX_FRAME_BYTES) {
        this.fail(new Error('引擎响应超过大小限制'))
        return
      }
      try {
        const frame = JSON.parse(line)
        if (
          frame.type !== 'response' ||
          frame.version !== PROTOCOL_VERSION ||
          typeof frame.id !== 'string' ||
          'error' in frame === 'result' in frame
        )
          throw new Error('Invalid response envelope')
        const pending = this.pending.get(frame.id)
        if (!pending) continue // A timed-out response cannot resolve another request.
        clearTimeout(pending.timer)
        this.pending.delete(frame.id)
        if (frame.error) pending.reject(new Error(`${frame.error.code}: ${frame.error.message}`))
        else pending.resolve(frame.result)
      } catch {
        this.fail(new Error('引擎返回了无效的协议数据'))
        return
      }
    }
    if (Buffer.byteLength(this.buffer) >= MAX_FRAME_BYTES)
      this.fail(new Error('引擎响应超过大小限制'))
  }

  private fail(error: Error): void {
    this.failure ??= error
    for (const pending of this.pending.values()) {
      clearTimeout(pending.timer)
      pending.reject(this.failure)
    }
    this.pending.clear()
    this.child?.kill()
  }

  async stop(): Promise<void> {
    const child = this.child
    if (!child || this.stopping) return
    // Attach before requesting shutdown: the child may exit immediately after its reply.
    let timer: ReturnType<typeof setTimeout>
    const exited = new Promise<void>((resolve) => {
      if (child.exitCode !== null || child.signalCode !== null) {
        resolve()
        return
      }
      const done = () => {
        clearTimeout(timer)
        resolve()
      }
      child.once('exit', done)
      timer = setTimeout(() => {
        child.kill()
        done()
      }, 1500)
    })
    if (!this.failure) {
      const shutdown = this.request('shutdown').catch(() => undefined)
      this.stopping = true
      await Promise.race([shutdown, exited])
    } else this.stopping = true
    child.stdin.end()
    await exited
    this.fail(new Error('引擎已停止'))
  }
}
