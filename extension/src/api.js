/*!
 * api.js — Raccoon HTTP API 客户端
 *
 * 契约来源：docs/04-API详细设计.md
 *   - 基础路径 /api/v1
 *   - Token 通道：Authorization: Bearer <token>（无 Cookie 上下文，豁免 CSRF）
 *   - POST /api/v1/tasks  body {items:[{url,quality,save_path}]}
 *   - GET  /api/v1/tasks  查询参数 status/platform/q/kind/limit/cursor
 *   - GET  /api/v1/tasks/{id}
 *   - 错误信封 {"error":{"code","message","request_id"}}
 *
 * 纯原生实现：无构建步骤、无 npm 依赖。以经典脚本方式挂载到 globalThis.RaccoonAPI，
 * service worker（importScripts）与 popup（<script>）共用同一份代码。
 */
(function (global) {
  'use strict';

  /** 基础路径（docs/04 §1「基础路径 /api/v1」）。 */
  var API_PREFIX = '/api/v1';
  /** 单次请求超时（毫秒）。 */
  var DEFAULT_TIMEOUT_MS = 15000;
  /** 后端单次最多接受 50 个任务（docs/04 §2.2）。 */
  var MAX_ITEMS_PER_REQUEST = 50;

  /** 错误码 → 中文提示（后端未给出 message 时兜底）。 */
  var ERROR_TEXT = {
    VALIDATION: '参数校验失败',
    UNAUTHORIZED: 'API Token 无效、已过期或已被撤销',
    FORBIDDEN: 'API Token 权限不足（scope 不含所需范围）',
    NOT_FOUND: '资源不存在',
    CONFLICT: '相同链接的任务已在队列中',
    SETUP_REQUIRED: '实例尚未初始化管理员账号',
    RATE_LIMITED: '请求过于频繁，请稍后再试',
    INTERNAL: '服务内部错误',
    ENGINE_UNAVAILABLE: '下载引擎不可用（yt-dlp 缺失或外部服务未配置）'
  };

  /**
   * API 错误（携带 HTTP 状态、业务码、request_id）。
   * @param {string} message
   * @param {{code?:string,status?:number,requestId?:string,existingTaskId?:string,body?:any}} info
   */
  function ApiError(message, info) {
    var err = Error.call(this, message) || this;
    this.name = 'ApiError';
    this.message = message;
    this.code = (info && info.code) || 'UNKNOWN';
    this.status = (info && info.status) || 0;
    this.requestId = (info && info.requestId) || '';
    this.existingTaskId = (info && info.existingTaskId) || '';
    this.body = info && info.body ? info.body : null;
    if (Error.captureStackTrace) Error.captureStackTrace(this, ApiError);
  }
  ApiError.prototype = Object.create(Error.prototype);
  ApiError.prototype.constructor = ApiError;

  function isApiError(err) {
    return !!err && err.name === 'ApiError';
  }

  /** 把异常翻译成人读的一句话。 */
  function describeError(err) {
    if (!err) return '未知错误';
    if (err.name === 'TypeError' && /fetch/i.test(err.message || '')) {
      return '无法连接到 Raccoon 实例（检查实例地址、网络与扩展站点权限）';
    }
    return err.message || ERROR_TEXT[err.code] || '请求失败';
  }

  /**
   * 规范化实例地址：补 scheme、去尾斜杠、保留端口与路径前缀。
   * @param {string} raw
   * @returns {string}
   */
  function normalizeBaseUrl(raw) {
    var s = String(raw == null ? '' : raw).trim();
    if (!s) throw new Error('请先填写 Raccoon 实例地址');
    // 显式写了 scheme 就按 scheme 校验（ftp:// 之类直接拒，别拼成 http://ftp//…）。
    var hasScheme = /^[a-z][a-z0-9+.\-]*:\/\//i.test(s);
    if (!hasScheme) s = 'http://' + s;
    var u;
    try {
      u = new URL(s);
    } catch (e) {
      throw new Error('实例地址不是合法 URL：' + raw);
    }
    if (u.protocol !== 'http:' && u.protocol !== 'https:') {
      throw new Error('实例地址只支持 http:// 或 https://');
    }
    var path = u.pathname === '/' ? '' : u.pathname.replace(/\/+$/, '');
    return u.origin + path;
  }

  /**
   * 由实例地址推导需要授权的站点权限匹配式（匹配式不含端口，Chromium 限制）。
   * @param {string} baseUrl
   * @returns {string}
   */
  function originPattern(baseUrl) {
    var u = new URL(normalizeBaseUrl(baseUrl));
    return u.protocol + '//' + u.hostname + '/*';
  }

  /**
   * 创建一个 API 客户端。
   * @param {{baseUrl:string, token:string, timeoutMs?:number}} config
   */
  function createClient(config) {
    var cfg = config || {};
    var client = {
      baseUrl: normalizeBaseUrl(cfg.baseUrl),
      token: String(cfg.token || '').trim(),
      timeoutMs: cfg.timeoutMs || DEFAULT_TIMEOUT_MS
    };
    if (!client.token) throw new Error('请先填写 API Token');

    /**
     * 统一的请求出口。
     * @param {string} method
     * @param {string} path 以 /api/v1 开头
     * @param {{query?:Object, body?:any}} opts
     */
    async function request(method, path, opts) {
      opts = opts || {};
      var url = client.baseUrl + API_PREFIX + path;
      if (opts.query) {
        var qs = new URLSearchParams();
        Object.keys(opts.query).forEach(function (key) {
          var v = opts.query[key];
          if (v === undefined || v === null || v === '') return;
          qs.append(key, String(v));
        });
        var qsStr = qs.toString();
        if (qsStr) url += '?' + qsStr;
      }

      var headers = { Accept: 'application/json' };
      if (client.token) headers.Authorization = 'Bearer ' + client.token;

      var init = {
        method: method,
        headers: headers,
        // Token 通道：不带 Cookie，天然规避会话通道的 CSRF 校验（docs/04 §1.1）。
        credentials: 'omit'
      };
      if (opts.body !== undefined) {
        headers['Content-Type'] = 'application/json';
        init.body = JSON.stringify(opts.body);
      }

      var ctrl = new AbortController();
      var timer = setTimeout(function () { ctrl.abort(); }, client.timeoutMs);
      init.signal = ctrl.signal;

      var res;
      try {
        res = await fetch(url, init);
      } catch (e) {
        clearTimeout(timer);
        if (e && e.name === 'AbortError') {
          throw new ApiError('请求超时（' + client.timeoutMs + 'ms）', { code: 'TIMEOUT' });
        }
        throw new ApiError('无法连接到 Raccoon 实例：' + ((e && e.message) || e), { code: 'NETWORK' });
      }
      clearTimeout(timer);

      var text = '';
      try {
        text = await res.text();
      } catch (e) {
        text = '';
      }
      var body = null;
      if (text) {
        try { body = JSON.parse(text); } catch (e) { body = null; }
      }

      if (!res.ok) {
        var err = (body && body.error) || {};
        var code = err.code || 'HTTP_' + res.status;
        var message = err.message || ERROR_TEXT[code] || ('HTTP ' + res.status);
        throw new ApiError(message, {
          code: code,
          status: res.status,
          requestId: err.request_id || '',
          existingTaskId: err.existing_task_id || (body && body.existing_task_id) || '',
          body: body
        });
      }
      return body;
    }

    return {
      baseUrl: client.baseUrl,
      token: client.token,

      /**
       * 提交下载任务。
       * POST /api/v1/tasks —— body {items:[{url,quality,save_path}]}
       * quality / save_path 传空串表示「用服务端默认」（docs/04 §2.2 中二者为可选字段）。
       * @param {Array<{url:string,quality?:string,savePath?:string}>} items
       */
      async submitTasks(items) {
        var list = Array.isArray(items) ? items : [];
        if (!list.length) throw new Error('没有可提交的链接');
        if (list.length > MAX_ITEMS_PER_REQUEST) {
          throw new Error('单次最多提交 ' + MAX_ITEMS_PER_REQUEST + ' 个任务');
        }
        var payload = list.map(function (it) {
          return {
            url: String(it.url || '').trim(),
            quality: String(it.quality || '').trim(),
            save_path: String(it.savePath || '').trim()
          };
        }).filter(function (entry) { return !!entry.url; });

        if (!payload.length) throw new Error('没有可提交的链接');

        var res = await request('POST', '/tasks', { body: { items: payload } });
        return {
          created: (res && res.data) || [],
          conflicts: (res && res.conflicts) || []
        };
      },

      /**
       * 查询任务列表。
       * GET /api/v1/tasks —— 过滤参数 status/platform/q/kind，分页 limit/cursor。
       * @param {{status?:string,platform?:string,q?:string,kind?:string,limit?:number,cursor?:string}} params
       */
      async listTasks(params) {
        var p = params || {};
        var res = await request('GET', '/tasks', {
          query: {
            status: p.status,
            platform: p.platform,
            q: p.q,
            kind: p.kind,
            limit: p.limit,
            cursor: p.cursor
          }
        });
        return {
          items: (res && res.items) || [],
          nextCursor: (res && res.next_cursor) || '',
          total: (res && res.total) || 0,
          counts: (res && res.counts) || {}
        };
      },

      /**
       * 查询任务详情。
       * GET /api/v1/tasks/{id}
       * @param {string} id
       */
      async getTask(id) {
        var res = await request('GET', '/tasks/' + encodeURIComponent(id));
        return {
          task: (res && res.data) || null,
          files: (res && res.files) || []
        };
      }
    };
  }

  global.RaccoonAPI = {
    API_PREFIX: API_PREFIX,
    MAX_ITEMS_PER_REQUEST: MAX_ITEMS_PER_REQUEST,
    ERROR_TEXT: ERROR_TEXT,
    ApiError: ApiError,
    isApiError: isApiError,
    describeError: describeError,
    normalizeBaseUrl: normalizeBaseUrl,
    originPattern: originPattern,
    createClient: createClient
  };
})(typeof globalThis !== 'undefined' ? globalThis : self);
