<template>
  <div class="ai-chat-container">
    <!-- 左侧会话列表 -->
    <div class="session-list">
      <div class="session-list-header">
        <span>会话列表</span>
        <button class="new-chat-btn" @click="createNewSession">＋ 新聊天</button>
      </div>
      <ul class="session-list-ul">
        <li
          v-for="session in sessions"
          :key="session.id"
          :class="['session-item', { active: currentSessionId === session.id }]"
          @click="switchSession(session.id)"
        >
          {{ session.name || `会话 ${session.id}` }}
        </li>
      </ul>
    </div>

    <!-- 右侧聊天区域 -->
    <div class="chat-section">
      <div class="top-bar">
        <button class="back-btn" @click="$router.push('/menu')">← 返回</button>
        <button class="sync-btn" @click="syncHistory" :disabled="!currentSessionId || tempSession">同步历史数据</button>
        <label for="modelType">选择模型：</label>
        <select id="modelType" v-model="selectedModel" class="model-select" :disabled="!tempSession && !!currentSessionId">
          <option v-for="model in availableModels" :key="model.id" :value="model.id" :disabled="!model.available">
            {{ model.name }}
          </option>
        </select>
        <label for="streamingMode" style="margin-left: 20px;">
          <input type="checkbox" id="streamingMode" v-model="isStreaming" />
          流式响应
        </label>
        <button class="upload-btn" @click="triggerFileUpload" :disabled="uploading">📎 上传文档(.md/.txt)</button>
        <input
          ref="fileInput"
          type="file"
          accept=".md,.txt,text/markdown,text/plain"
          style="display: none"
          @change="handleFileUpload"
        />
      </div>
      <div v-if="ragDocuments.length" class="knowledge-bar">
        <span v-for="doc in ragDocuments" :key="doc.id" class="knowledge-item">
          {{ doc.name }} · {{ doc.status }}
          <button v-if="doc.status === 'failed'" title="重试索引" @click="retryDocument(doc.id)">↻</button>
          <button title="删除文档" @click="deleteDocument(doc.id)">×</button>
        </span>
      </div>

      <div class="chat-messages" ref="messagesRef">
        <button v-if="hasMoreHistory" class="load-older-btn" :disabled="historyLoading" @click="loadOlderHistory">
          {{ historyLoading ? '加载中...' : '加载更早消息' }}
        </button>
        <div
          v-for="(message, index) in currentMessages"
      :key="message.id || `${message.role}-${index}`"
          :class="['message', message.role === 'user' ? 'user-message' : 'ai-message']"
        >
          <div class="message-header">
            <b>{{ message.role === 'user' ? '你' : 'AI' }}:</b>
            <button v-if="message.role === 'assistant'" class="tts-btn" @click="playTTS(message.content)">🔊</button>
            <span v-if="message.meta && message.meta.status === 'streaming'" class="streaming-indicator"> ··</span>
            <span v-if="message.meta && ['failed', 'canceled', 'interrupted'].includes(message.meta.status)" class="failed-indicator">
              {{ message.meta.status === 'canceled' ? '已取消' : message.meta.status === 'interrupted' ? '已中断' : '生成失败' }}
            </span>
          </div>
          <div class="message-content" v-html="renderMarkdown(message.content)"></div>
          <div v-if="message.meta?.citations?.length" class="message-citations">
            <span v-for="citation in message.meta.citations" :key="citation">{{ citation }}</span>
          </div>
        </div>
      </div>

      <div class="chat-input">
        <textarea
          v-model="inputMessage"
          placeholder="请输入你的问题..."
          @keydown.enter.exact.prevent="sendMessage"
          :disabled="loading"
          ref="messageInput"
          rows="1"
        ></textarea>
        <button
          type="button"
          :disabled="!inputMessage.trim() || loading"
          @click="sendMessage"
          class="send-btn"
        >
          {{ loading ? '发送中...' : '发送' }}
        </button>
        <button v-if="loading && isStreaming" type="button" class="stop-btn" @click="stopStreaming">停止</button>
      </div>
    </div>
  </div>
</template>

<script>


import { ref, nextTick, computed, onMounted, onBeforeUnmount } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../utils/api'

export default {
  name: 'AIChat',
  setup() {

    const sessions = ref({})
    // [AI聊天修复-会话状态] 页面首次进入时按新会话处理，避免发送消息时访问不存在的会话。
    const currentSessionId = ref('temp')
    const tempSession = ref(true)
    const currentMessages = ref([])
    const inputMessage = ref('')
    const loading = ref(false)
    const messagesRef = ref(null)
    const messageInput = ref(null)
    const selectedModel = ref('1')
    const availableModels = ref([])
    const isStreaming = ref(false)
    const uploading = ref(false)
    const fileInput = ref(null)
    const ragDocuments = ref([])
    // [第三阶段优化-流式] 保存当前请求控制器，组件退出时主动通知后端取消模型生成。
    let streamAbortController = null
  const historyLoading = ref(false)
  const hasMoreHistory = ref(false)
  const historyPageSize = 50

  // [第二阶段优化-历史] 将数据库消息状态转换为前端消息模型。
  const mapHistory = (items) => items.map(item => ({
    id: item.id,
    role: item.is_user ? 'user' : 'assistant',
    content: item.content || item.error_message || (item.status === 'failed' ? '模型生成失败，请重试' : ''),
    meta: {
      status: item.status || 'completed',
      createdAt: item.created_at,
      citations: item.citations || [],
      usage: {
        promptTokens: item.prompt_tokens || 0,
        completionTokens: item.completion_tokens || 0,
        latencyMs: item.latency_ms || 0
      }
    }
  }))


    const renderMarkdown = (text) => {
      if (!text && text !== '') return ''
      return String(text)
        .replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>')
        .replace(/\*(.*?)\*/g, '<em>$1</em>')
        .replace(/`(.*?)`/g, '<code>$1</code>')
        .replace(/\n/g, '<br>')
    }

    const playTTS = async (text) => {
      try {
        // 创建TTS任务
        const createResponse = await api.post('/AI/chat/tts', { text })
        if (createResponse.data && createResponse.data.status_code === 1000 && createResponse.data.task_id) {
          const taskId = createResponse.data.task_id
          
          // 先等待5秒钟再开始轮询
          await new Promise(resolve => setTimeout(resolve, 5000))
          
          // 轮询查询任务结果
          const maxAttempts = 30
          const pollInterval = 2000
          let attempts = 0
          
          const pollResult = async () => {
            const queryResponse = await api.get('/AI/chat/tts/query', { params: { task_id: taskId } })
            
            if (queryResponse.data && queryResponse.data.status_code === 1000) {
              const taskStatus = queryResponse.data.task_status
                
              if (taskStatus === 'Success' && queryResponse.data.task_result) {
                // 任务完成，播放音频
                // 后端返回的 task_result 是直接的 URL 字符串
                const audio = new Audio(queryResponse.data.task_result)
                audio.play()
                return true
              } else if (taskStatus === 'Running' ||taskStatus === 'Created' ) {
                // 任务进行中，继续轮询
                attempts++
                if (attempts < maxAttempts) {
                  await new Promise(resolve => setTimeout(resolve, pollInterval))
                  return await pollResult()
                } else {
                  ElMessage.error('语音合成超时')
                  return true
                }
              } else {
                // 其他状态（如失败）
                ElMessage.error('语音合成失败')
                return true
              }
            }
            
            attempts++
            if (attempts < maxAttempts) {
              await new Promise(resolve => setTimeout(resolve, pollInterval))
              return await pollResult()
            } else {
              ElMessage.error('语音合成超时')
              return true
            }
          }
          
          await pollResult()
        } else {
          ElMessage.error('无法创建语音合成任务')
        }
      } catch (error) {
        console.error('TTS error:', error)
        ElMessage.error('请求语音接口失败')
      }
    }

    const loadSessions = async () => {
      try {
        const response = await api.get('/AI/chat/sessions')
        if (response.data && response.data.status_code === 1000 && Array.isArray(response.data.sessions)) {
          const sessionMap = {}
          response.data.sessions.forEach(s => {
            const sid = String(s.sessionId)
            sessionMap[sid] = {
              id: sid,
              name: s.name || `会话 ${sid}`,
        modelType: s.modelType || '1',
        messages: [], // lazy load
        historyPage: 0,
        hasMore: false
            }
          })
          sessions.value = sessionMap
        }
      } catch (error) {
        console.error('Load sessions error:', error)
      }
    }

    const loadModels = async () => {
      try {
        const response = await api.get('/AI/models')
        if (response.data?.status_code === 1000 && Array.isArray(response.data.models)) {
          availableModels.value = response.data.models
        }
      } catch (error) {
        console.error('Load models error:', error)
        availableModels.value = [
          { id: '1', name: '阿里百炼', available: true },
          { id: '2', name: '阿里百炼 RAG', available: true },
          { id: '3', name: '阿里百炼 MCP', available: true }
        ]
      }
    }

    const loadDocuments = async () => {
      try {
        const response = await api.get('/file/documents')
        ragDocuments.value = response.data?.documents || []
      } catch (error) {
        console.error('Load documents error:', error)
      }
    }

    const deleteDocument = async (documentId) => {
      if (!window.confirm('确定删除该知识库文档吗？')) return
      await api.delete(`/file/documents/${documentId}`)
      await loadDocuments()
    }

    const retryDocument = async (documentId) => {
      await api.post(`/file/documents/${documentId}/retry`)
      await loadDocuments()
    }

    const createNewSession = () => {
      if (loading.value) {
        ElMessage.warning('请等待当前回答完成')
        return
      }
      currentSessionId.value = 'temp'
      tempSession.value = true
      currentMessages.value = []
      hasMoreHistory.value = false
      // focus input
      nextTick(() => {
        if (messageInput.value) messageInput.value.focus()
      })
    }

    const switchSession = async (sessionId) => {
      if (!sessionId) return
      if (loading.value) {
        ElMessage.warning('请等待当前回答完成')
        return
      }
      const normalizedSessionId = String(sessionId)
      const targetSession = sessions.value[normalizedSessionId]
      if (!targetSession) {
        ElMessage.error('会话不存在或已失效，请刷新后重试')
        return
      }
      currentSessionId.value = normalizedSessionId
      tempSession.value = false
      selectedModel.value = targetSession.modelType || '1'

      // lazy load history if not present
    if (!targetSession.messages || targetSession.messages.length === 0) {
    await loadHistoryPage(normalizedSessionId, 1, false)
    }

      const refreshedSession = sessions.value[normalizedSessionId]
      if (!refreshedSession) return
      currentMessages.value = [...(refreshedSession.messages || [])]
    hasMoreHistory.value = refreshedSession.hasMore
      await nextTick()
      scrollToBottom()
    }

    const syncHistory = async () => {
      if (!currentSessionId.value || tempSession.value) {
        ElMessage.warning('请选择已有会话进行同步')
        return
      }
    const sessionId = String(currentSessionId.value)
    await loadHistoryPage(sessionId, 1, false)
    const currentSession = sessions.value[sessionId]
    if (!currentSession) {
      ElMessage.error('当前会话不存在，请重新选择')
      return
    }
    currentMessages.value = [...(currentSession.messages || [])]
    await nextTick()
    scrollToBottom()
    }

  // [第二阶段优化-历史] 分页加载数据库历史；加载旧页时前插，保持时间顺序。
  async function loadHistoryPage(sessionId, page, prepend) {
    if (historyLoading.value) return
    historyLoading.value = true
    try {
    const response = await api.post('/AI/chat/history', {
      sessionId,
      page,
      pageSize: historyPageSize
    })
    if (!response.data || response.data.status_code !== 1000 || !Array.isArray(response.data.history)) {
      throw new Error('Invalid history response')
    }
    const messages = mapHistory(response.data.history)
    const session = sessions.value[sessionId]
    if (!session) {
      throw new Error('当前会话不存在')
    }
    session.messages = prepend ? [...messages, ...(session.messages || [])] : messages
    session.historyPage = page
    session.hasMore = Boolean(response.data.hasMore)
    hasMoreHistory.value = session.hasMore
    } catch (err) {
    console.error('Load history error:', err)
    ElMessage.error(err.response?.data?.status_msg || '请求历史数据失败')
    } finally {
    historyLoading.value = false
    }
  }

  const loadOlderHistory = async () => {
    const sessionId = currentSessionId.value
    if (!sessionId || !sessions.value[sessionId]?.hasMore) return
    await loadHistoryPage(sessionId, sessions.value[sessionId].historyPage + 1, true)
    const currentSession = sessions.value[sessionId]
    if (!currentSession) return
    currentMessages.value = [...(currentSession.messages || [])]
  }


    const sendMessage = async () => {
      if (!inputMessage.value || !inputMessage.value.trim()) {
        ElMessage.warning('请输入消息内容')
        return
      }

      const userMessage = {
        role: 'user',
        content: inputMessage.value
      }
      const currentInput = inputMessage.value
      inputMessage.value = ''


      currentMessages.value.push(userMessage)
      await nextTick()
      scrollToBottom()

      try {
        loading.value = true
        if (isStreaming.value) {

          await handleStreaming(currentInput)
        } else {

          await handleNormal(currentInput)
        }
      } catch (err) {
        console.error('Send message error:', err)
        ElMessage.error('发送失败，请重试')

        if (!tempSession.value && currentSessionId.value && sessions.value[currentSessionId.value] && sessions.value[currentSessionId.value].messages) {

          const sessionArr = sessions.value[currentSessionId.value].messages
          if (sessionArr && sessionArr.length) sessionArr.pop()
        }
        currentMessages.value.pop()
      } finally {
        if (!isStreaming.value) {
          loading.value = false
        }
        await nextTick()
        scrollToBottom()
      }
    }


    async function handleStreaming(question) {
      const abortController = new AbortController()
      streamAbortController = abortController

      const aiMessage = {
        role: 'assistant',
        content: '',
        meta: { status: 'streaming' } // mark streaming
      }


      const aiMessageIndex = currentMessages.value.length
      currentMessages.value.push(aiMessage)

      if (!tempSession.value && currentSessionId.value && sessions.value[currentSessionId.value]) {
        if (!sessions.value[currentSessionId.value].messages) sessions.value[currentSessionId.value].messages = []
		// [第二阶段优化-一致性] 流式请求同时写入本地用户消息，和数据库顺序保持一致。
		sessions.value[currentSessionId.value].messages.push({ role: 'user', content: question })
        sessions.value[currentSessionId.value].messages.push({ role: 'assistant', content: '' })
      }


      const url = tempSession.value
        ? '/api/AI/chat/send-stream-new-session'  
        : '/api/AI/chat/send-stream'           

      const headers = {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${localStorage.getItem('token') || ''}`
      }

      const body = tempSession.value
        ? { question: question, modelType: selectedModel.value }
        : { question: question, modelType: selectedModel.value, sessionId: currentSessionId.value }

      try {
        // 创建 fetch 连接读取 SSE 流
        const response = await fetch(url, {
          method: 'POST',
          headers,
          body: JSON.stringify(body),
          signal: abortController.signal
        })

        if (!response.ok) {
          loading.value = false
          throw new Error('Network response was not ok')
        }

        const reader = response.body.getReader()
        const decoder = new TextDecoder()
        let buffer = ''

        // 读取流数据
        // eslint-disable-next-line no-constant-condition
        while (true) {
          const { done, value } = await reader.read()
          if (done) break

          const chunk = decoder.decode(value, { stream: true })
          buffer += chunk

          // 按行分割
          const lines = buffer.split('\n')
          buffer = lines.pop() || '' // 保留未完成的行

          for (const line of lines) {
            const trimmedLine = line.trim()
            if (!trimmedLine) continue

            // 处理 SSE 格式：data: <content>
            if (trimmedLine.startsWith('data:')) {
              const data = trimmedLine.slice(5).trim()
              console.log('[SSE] Received:', data) // 调试日志

              if (data === '[DONE]') {
                // 流结束
                console.log('[SSE] Stream done')
                loading.value = false
                currentMessages.value[aiMessageIndex].meta = { status: 'done' }
                currentMessages.value = [...currentMessages.value]
              } else if (data.startsWith('{')) {
                // 尝试解析 JSON（如 sessionId）
                try {
                  const parsed = JSON.parse(data)
                  if (parsed.sessionId) {
                    const newSid = String(parsed.sessionId)
                    console.log('[SSE] Session ID:', newSid)
                    if (tempSession.value) {
                      sessions.value[newSid] = {
                        id: newSid,
                        name: '新会话',
            modelType: selectedModel.value,
            messages: [...currentMessages.value],
            historyPage: 1,
            hasMore: false
                      }
                      currentSessionId.value = newSid
                      tempSession.value = false
                    }
                  } else if (parsed.type === 'chunk' && typeof parsed.content === 'string') {
                    // [第三阶段优化-流式] JSON 消息可安全承载换行和以“{”开头的正文。
                    currentMessages.value[aiMessageIndex].content += parsed.content
                  } else if (parsed.type === 'citation') {
                    currentMessages.value[aiMessageIndex].meta.citations = parsed.citations || []
                  } else if (parsed.type === 'usage') {
                    currentMessages.value[aiMessageIndex].meta.usage = parsed
                  } else if (parsed.message) {
                    throw new Error(parsed.message)
                  }
                } catch (e) {
                  // 不是 JSON，当作普通文本处理
                  currentMessages.value[aiMessageIndex].content += data
                  console.log('[SSE] Content updated:', currentMessages.value[aiMessageIndex].content.length)
                }
              } else {
                // 普通文本数据，直接追加
                // 使用数组索引直接更新，强制 Vue 响应式系统检测变化
                currentMessages.value[aiMessageIndex].content += data
                console.log('[SSE] Content updated:', currentMessages.value[aiMessageIndex].content.length)
              }

              // 每收到一条数据就立即更新 DOM
              // 强制更新整个数组以触发响应式
              currentMessages.value = [...currentMessages.value]
              
              // 使用 requestAnimationFrame 强制浏览器重排
              await new Promise(resolve => {
                requestAnimationFrame(() => {
                  scrollToBottom()
                  resolve()
                })
              })
            }
          }
        }

        // 流读取完成后的处理
        loading.value = false
        currentMessages.value[aiMessageIndex].meta = { status: 'done' }
        currentMessages.value = [...currentMessages.value]

        // 同步到 sessions 存储
        if (!tempSession.value && currentSessionId.value && sessions.value[currentSessionId.value]) {
          const sessMsgs = sessions.value[currentSessionId.value].messages
          if (Array.isArray(sessMsgs) && sessMsgs.length) {
            const lastIndex = sessMsgs.length - 1
            if (sessMsgs[lastIndex] && sessMsgs[lastIndex].role === 'assistant') {
              sessMsgs[lastIndex].content = currentMessages.value[aiMessageIndex].content
            }
          }
        }
      } catch (err) {
        console.error('Stream error:', err)
        loading.value = false
        if (err.name === 'AbortError') {
          currentMessages.value[aiMessageIndex].meta = { status: 'canceled' }
          if (!currentMessages.value[aiMessageIndex].content) {
            currentMessages.value[aiMessageIndex].content = '生成已停止'
          }
          currentMessages.value = [...currentMessages.value]
          return
        }
        currentMessages.value[aiMessageIndex].content = '模型生成失败，请重试'
        currentMessages.value[aiMessageIndex].meta = { status: 'failed' }
        if (!tempSession.value && currentSessionId.value && sessions.value[currentSessionId.value]) {
          const sessionMessages = sessions.value[currentSessionId.value].messages
          const lastMessage = sessionMessages[sessionMessages.length - 1]
          if (lastMessage && lastMessage.role === 'assistant') {
            lastMessage.content = '模型生成失败，请重试'
            lastMessage.meta = { status: 'failed' }
          }
        }
        currentMessages.value = [...currentMessages.value]
        ElMessage.error('流式传输出错')
      } finally {
        if (streamAbortController === abortController) {
          streamAbortController = null
        }
      }
    }

    const stopStreaming = () => {
      if (streamAbortController) {
        streamAbortController.abort()
      }
    }


    async function handleNormal(question) {
      if (tempSession.value) {

        const response = await api.post('/AI/chat/send-new-session', {
          question: question,
          modelType: selectedModel.value
        })
        if (response.data && response.data.status_code === 1000) {
          if (!response.data.sessionId) {
            throw new Error('创建会话成功，但响应中缺少 sessionId')
          }
          const sessionId = String(response.data.sessionId)
          const aiMessage = {
            role: 'assistant',
            content: response.data.Information || ''
          }

          sessions.value[sessionId] = {
            id: sessionId,
            name: '新会话',
      modelType: selectedModel.value,
      messages: [ { role: 'user', content: question }, aiMessage ],
      historyPage: 1,
      hasMore: false
          }
          currentSessionId.value = sessionId
          tempSession.value = false
          currentMessages.value = [...sessions.value[sessionId].messages]
        } else {
          ElMessage.error(response.data?.status_msg || '发送失败')

          currentMessages.value.pop()
        }
      } else {
        const sessionId = String(currentSessionId.value || '')
        const currentSession = sessions.value[sessionId]
        if (!currentSession) {
          // [AI聊天修复-会话状态] 本地会话已失效时回到新会话，不访问 undefined.messages。
          currentSessionId.value = 'temp'
          tempSession.value = true
          throw new Error('当前会话不存在，请重新发送')
        }
        if (!Array.isArray(currentSession.messages)) {
          currentSession.messages = []
        }
        const sessionMsgs = currentSession.messages

        sessionMsgs.push({ role: 'user', content: question })

        const response = await api.post('/AI/chat/send', {
          question: question,
          modelType: selectedModel.value,
          sessionId
        })
        if (response.data && response.data.status_code === 1000) {
          const aiMessage = { role: 'assistant', content: response.data.Information || '' }
          sessionMsgs.push(aiMessage)
          currentMessages.value = [...sessionMsgs]
        } else {
          ElMessage.error(response.data?.status_msg || '发送失败')
          sessionMsgs.pop() // rollback
          currentMessages.value.pop()
        }
      }
    }


    const scrollToBottom = () => {
      if (messagesRef.value) {
        try {
          messagesRef.value.scrollTop = messagesRef.value.scrollHeight
        } catch (e) {
          // ignore
        }
      }
    }

    const triggerFileUpload = () => {
      if (fileInput.value) {
        fileInput.value.click()
      }
    }

    const handleFileUpload = async (event) => {
      const file = event.target.files[0]
      if (!file) return

      // 前端校验：只允许.md或.txt文件
      const fileName = file.name.toLowerCase()
      if (!fileName.endsWith('.md') && !fileName.endsWith('.txt')) {
        ElMessage.error('只允许上传 .md 或 .txt 文件')
        // 清空文件输入
        if (fileInput.value) {
          fileInput.value.value = ''
        }
        return
      }

      try {
        uploading.value = true
        const formData = new FormData()
        formData.append('file', file)

        const response = await api.post('/file/upload', formData, {
          headers: {
            'Content-Type': 'multipart/form-data'
          }
        })

        if (response.data && response.data.status_code === 1000) {
          ElMessage.success(`文件上传成功`)
          await loadDocuments()
        } else {
          ElMessage.error(response.data?.status_msg || '上传失败')
        }
      } catch (error) {
        console.error('File upload error:', error)
        ElMessage.error('文件上传失败')
      } finally {
        await loadDocuments()
        uploading.value = false
        // 清空文件输入
        if (fileInput.value) {
          fileInput.value.value = ''
        }
      }
    }

    onMounted(() => {
      Promise.all([loadSessions(), loadModels(), loadDocuments()])
    })
    onBeforeUnmount(() => {
      // [第三阶段优化-流式] 页面离开时中止 fetch，Request.Context 会继续取消上游模型调用。
      if (streamAbortController) {
        streamAbortController.abort()
        streamAbortController = null
      }
    })

    // expose to template
    return {
      sessions: computed(() => Object.values(sessions.value)),
      currentSessionId,
      tempSession,
      currentMessages,
      inputMessage,
      loading,
      messagesRef,
      messageInput,
      selectedModel,
      availableModels,
      isStreaming,
      uploading,
      fileInput,
      ragDocuments,
    historyLoading,
    hasMoreHistory,
      renderMarkdown,
      playTTS,
      createNewSession,
      switchSession,
      syncHistory,
    loadOlderHistory,
      sendMessage,
      stopStreaming,
      triggerFileUpload,
      handleFileUpload,
      deleteDocument,
      retryDocument
    }
  }
}
</script>

<style scoped>
.ai-chat-container {
  height: 100vh;
  display: flex;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  position: relative;
  overflow: hidden;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial;
  color: #222;
}

.ai-chat-container::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: url('data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><circle cx="20" cy="20" r="2" fill="rgba(255,255,255,0.08)"/><circle cx="80" cy="80" r="2" fill="rgba(255,255,255,0.08)"/><circle cx="40" cy="60" r="1" fill="rgba(255,255,255,0.06)"/><circle cx="60" cy="30" r="1.5" fill="rgba(255,255,255,0.06)"/></svg>');
  animation: float 20s ease-in-out infinite;
  opacity: 0.25;
}

@keyframes float {
  0%, 100% { transform: translateY(0px) rotate(0deg); }
  50% { transform: translateY(-20px) rotate(180deg); }
}

.session-list {
  width: 280px;
  height: 100vh;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  background: rgba(255, 255, 255, 0.95);
  backdrop-filter: blur(15px);
  border-right: 1px solid rgba(0, 0, 0, 0.08);
  box-shadow: 2px 0 20px rgba(0, 0, 0, 0.08);
  position: relative;
  z-index: 2;
}

.session-list-header {
  padding: 20px;
  text-align: center;
  font-weight: 600;
  background: linear-gradient(135deg, rgba(102, 126, 234, 0.06) 0%, rgba(103, 194, 58, 0.06) 100%);
  border-bottom: 1px solid rgba(0, 0, 0, 0.06);
  display: flex;
  flex-direction: column;
  gap: 12px;
  align-items: center;
}

.new-chat-btn {
  width: 100%;
  padding: 12px 0;
  cursor: pointer;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
  border: none;
  border-radius: 12px;
  font-size: 14px;
  font-weight: 600;
  box-shadow: 0 4px 15px rgba(102, 126, 234, 0.28);
  transition: all 0.25s ease;
  position: relative;
  overflow: hidden;
}

.new-chat-btn::before {
  content: '';
  position: absolute;
  top: 0;
  left: -100%;
  width: 100%;
  height: 100%;
  background: linear-gradient(90deg, transparent, rgba(255,255,255,0.12), transparent);
  transition: left 0.5s;
}

.new-chat-btn:hover::before {
  left: 100%;
}

.new-chat-btn:hover {
  transform: translateY(-2px);
  box-shadow: 0 8px 25px rgba(102, 126, 234, 0.36);
}

.session-list-ul {
  list-style: none;
  padding: 0;
  margin: 0;
  flex: 1;
  overflow-y: auto;
}

.session-item {
  padding: 15px 20px;
  cursor: pointer;
  border-bottom: 1px solid rgba(0, 0, 0, 0.03);
  transition: all 0.2s ease;
  position: relative;
  color: #2c3e50;
}

.session-item.active {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
  font-weight: 600;
  box-shadow: inset 0 0 20px rgba(102, 126, 234, 0.2);
}

.session-item:hover {
  background: rgba(102, 126, 234, 0.06);
  transform: translateX(4px);
}

/* chat section */
.chat-section {
  flex: 1;
  display: flex;
  flex-direction: column;
  position: relative;
  z-index: 1;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}

.top-bar {
  background: rgba(255, 255, 255, 0.95);
  backdrop-filter: blur(10px);
  color: #2c3e50;
  display: flex;
  align-items: center;
  padding: 12px 24px;
  box-shadow: 0 2px 14px rgba(0, 0, 0, 0.06);
  border-bottom: 1px solid rgba(0, 0, 0, 0.06);
  gap: 12px;
}

.back-btn {
  background: rgba(255, 255, 255, 0.22);
  border: 1px solid rgba(0, 0, 0, 0.06);
  color: #2c3e50;
  padding: 8px 14px;
  border-radius: 10px;
  cursor: pointer;
  font-weight: 600;
  transition: all 0.2s ease;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
}

.back-btn:hover {
  background: rgba(255, 255, 255, 0.32);
  transform: translateY(-2px);
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.08);
}

.sync-btn {
  background: linear-gradient(135deg, #67c23a 0%, #409eff 100%);
  color: white;
  padding: 8px 14px;
  border: none;
  border-radius: 10px;
  cursor: pointer;
  font-size: 13px;
  font-weight: 600;
  box-shadow: 0 4px 12px rgba(103, 194, 58, 0.2);
  transition: all 0.2s ease;
}

.sync-btn:disabled {
  background: #ccc;
  box-shadow: none;
  cursor: not-allowed;
}

.model-select {
  margin-left: 6px;
  padding: 6px 10px;
  border: 1px solid rgba(0, 0, 0, 0.06);
  border-radius: 8px;
  background: white;
  color: #2c3e50;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s ease;
}

.model-select:disabled {
  background: #f1f3f5;
  color: #6b7280;
  cursor: not-allowed;
}

.upload-btn {
  background: linear-gradient(135deg, #f093fb 0%, #f5576c 100%);
  color: white;
  padding: 8px 14px;
  border: none;
  border-radius: 10px;
  cursor: pointer;
  font-size: 13px;
  font-weight: 600;
  box-shadow: 0 4px 12px rgba(245, 87, 108, 0.2);
  transition: all 0.2s ease;
}

.upload-btn:hover:not(:disabled) {
  transform: translateY(-2px);
  box-shadow: 0 6px 16px rgba(245, 87, 108, 0.3);
}

.upload-btn:disabled {
  background: #ccc;
  box-shadow: none;
  cursor: not-allowed;
}

.chat-messages {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 30px;
  display: flex;
  flex-direction: column;
  gap: 18px;
  position: relative;
  z-index: 1;
}

/* [第二阶段优化-历史] 分页和失败状态的轻量反馈。 */
.load-older-btn {
  align-self: center;
  padding: 7px 12px;
  border: 1px solid #cbd5e1;
  border-radius: 6px;
  background: #fff;
  color: #334155;
  cursor: pointer;
}

.stop-btn {
  flex: 0 0 auto;
  border: 1px solid #d9534f;
  background: #fff;
  color: #b52b27;
  padding: 10px 14px;
  cursor: pointer;
}

.message-citations {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 8px;
  font-size: 12px;
  color: #52616b;
}

.message-citations span {
  border-left: 2px solid #409eff;
  padding-left: 6px;
}

.knowledge-bar {
  display: flex;
  gap: 8px;
  overflow-x: auto;
  padding: 6px 16px;
  border-bottom: 1px solid #e8edf2;
  background: #f8fafc;
}

.knowledge-item {
  flex: 0 0 auto;
  font-size: 12px;
  color: #425466;
}

.knowledge-item button {
  border: 0;
  background: transparent;
  cursor: pointer;
  color: #5b6b7a;
}

.load-older-btn:disabled {
  cursor: wait;
  opacity: 0.65;
}

.failed-indicator {
  margin-left: 8px;
  color: #b42318;
  font-size: 12px;
  font-weight: 600;
}

/* scrollbar */
.chat-messages::-webkit-scrollbar {
  width: 8px;
}
.chat-messages::-webkit-scrollbar-thumb {
  background: rgba(0,0,0,0.12);
  border-radius: 8px;
}
.chat-messages::-webkit-scrollbar-track {
  background: transparent;
}

.message {
  max-width: 70%;
  padding: 14px 18px;
  border-radius: 18px;
  line-height: 1.6;
  word-wrap: break-word;
  position: relative;
  animation: messageSlideIn 0.28s ease-out;
  font-size: 15px;
  box-sizing: border-box;
}

@keyframes messageSlideIn {
  from {
    opacity: 0;
    transform: translateY(12px) scale(0.98);
  }
  to {
    opacity: 1;
    transform: translateY(0) scale(1);
  }
}

.user-message {
  align-self: flex-end;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
  box-shadow: 0 6px 20px rgba(102, 126, 234, 0.16);
}

.user-message::after {
  content: '';
  position: absolute;
  bottom: -6px;
  right: 18px;
  width: 0;
  height: 0;
  border-left: 8px solid transparent;
  border-right: 8px solid transparent;
  border-top: 8px solid #764ba2;
}

.ai-message {
  align-self: flex-start;
  background: rgba(255, 255, 255, 0.95);
  backdrop-filter: blur(4px);
  color: #2c3e50;
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.06);
  border: 1px solid rgba(255, 255, 255, 0.3);
}

.ai-message::after {
  content: '';
  position: absolute;
  bottom: -6px;
  left: 18px;
  width: 0;
  height: 0;
  border-left: 8px solid transparent;
  border-right: 8px solid transparent;
  border-top: 8px solid rgba(255, 255, 255, 0.95);
}

.message-header {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 8px;
}

.message-header b {
  font-weight: 600;
}

.tts-btn {
  padding: 6px 10px;
  border-radius: 8px;
  font-size: 12px;
  cursor: pointer;
  background: linear-gradient(135deg, #67c23a 0%, #409eff 100%);
  color: white;
  border: none;
  transition: all 0.18s ease;
  box-shadow: 0 2px 8px rgba(103, 194, 58, 0.18);
}

.tts-btn:hover {
  transform: scale(1.05);
  box-shadow: 0 4px 12px rgba(103, 194, 58, 0.25);
}

.streaming-indicator {
  color: #999;
  font-weight: 600;
  margin-left: 6px;
}

/* message content */
.message-content {
  white-space: pre-wrap;
  word-break: break-word;
}

/* input area */
.chat-input {
  padding: 24px;
  background: rgba(255, 255, 255, 0.96);
  backdrop-filter: blur(8px);
  border-top: 1px solid rgba(0, 0, 0, 0.06);
  position: relative;
  z-index: 1;
}

.chat-input textarea {
  width: 100%;
  resize: none;
  border: 2px solid rgba(0, 0, 0, 0.06);
  border-radius: 12px;
  padding: 14px 16px;
  font-size: 15px;
  outline: none;
  background: rgba(255,255,255,0.96);
  color: #2c3e50;
  transition: all 0.18s ease;
  min-height: 20px;
  max-height: 160px;
  box-shadow: 0 2px 10px rgba(0,0,0,0.04);
}

.chat-input textarea:focus {
  border-color: #409eff;
  box-shadow: 0 8px 30px rgba(64,158,255,0.06);
  transform: translateY(-1px);
}

.send-btn {
  position: absolute;
  right: 36px;
  bottom: 30px;
  padding: 12px 22px;
  border: none;
  border-radius: 50px;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
  font-size: 15px;
  font-weight: 600;
  cursor: pointer;
  box-shadow: 0 6px 20px rgba(102,126,234,0.18);
  transition: all 0.18s ease;
}

.send-btn:hover:not(:disabled) {
  transform: translateY(-3px) scale(1.02);
}

.send-btn:disabled {
  background: #ccc;
  box-shadow: none;
  cursor: not-allowed;
}
</style>
