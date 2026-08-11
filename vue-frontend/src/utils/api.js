import axios from 'axios'

const api = axios.create({
  baseURL: '/api', // 使用代理路径，开发环境会自动代理到后端
  timeout: 0  //不启用超时机制
})

// 请求拦截器
api.interceptors.request.use(
  config => {
    const token = localStorage.getItem('token')
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    return config
  },
  error => {
    return Promise.reject(error)
  }
)

// 响应拦截器
api.interceptors.response.use(
  response => {
    return response
  },
  error => {
	// [第一阶段优化-HTTP] 仅无效 Token 才清理登录态，普通 401 错误保留服务端提示。
    const isInvalidToken = error.response &&
      error.response.status === 401 &&
      error.response.data &&
      error.response.data.status_code === 2006
    if (isInvalidToken) {
      localStorage.removeItem('token')
      window.location.href = '/login'
    }
    return Promise.reject(error)
  }
)

export default api
