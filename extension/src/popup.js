/*!
 * popup.js — 扩展弹窗逻辑
 *
 * 三块能力（docs/01 §3.6）：
 *   1. 配置面板：实例地址 / API Token / 默认质量 / 默认保存目录 → chrome.storage.local；
 *   2. 当前页面链接一键提交 → 交由 background 统一走 POST /api/v1/tasks；
 *   3. 任务状态查询 → GET /api/v1/tasks。
 *
 * 经典脚本：依赖 api.js / settings.js 先行加载。
 */
(function () {
  'use strict';

  /** 任务状态 → 中文标签（docs/01 §4 七态）。 */
  var STATUS_TEXT = {
    waiting: '等待中',
    parsing: '解析中',
    downloading: '下载中',
    post: '后处理',
    done: '已完成',
    failed: '失败',
    cancelled: '已取消'
  };

  var el = {
    instanceLabel: document.getElementById('instanceLabel'),
    pageUrl: document.getElementById('pageUrl'),
    btnSubmit: document.getElementById('btnSubmit'),
    btnRefreshPage: document.getElementById('btnRefreshPage'),
    submitMsg: document.getElementById('submitMsg'),
    statusFilter: document.getElementById('statusFilter'),
    btnReload: document.getElementById('btnReload'),
    taskList: document.getElementById('taskList'),
    taskEmpty: document.getElementById('taskEmpty'),
    taskMsg: document.getElementById('taskMsg'),
    btnToggleConfig: document.getElementById('btnToggleConfig'),
    configBody: document.getElementById('configBody'),
    cfgBaseUrl: document.getElementById('cfgBaseUrl'),
    cfgToken: document.getElementById('cfgToken'),
    cfgQuality: document.getElementById('cfgQuality'),
    cfgSavePath: document.getElementById('cfgSavePath'),
    btnSave: document.getElementById('btnSave'),
    btnTest: document.getElementById('btnTest'),
    cfgMsg: document.getElementById('cfgMsg'),
    footStatus: document.getElementById('footStatus'),
    linkDocs: document.getElementById('linkDocs')
  };

  /** 当前页面的 tab（提交时传给 background 做媒体链接捕获）。 */
  var currentTab = null;

  /** 缓存的配置。 */
  var config = { baseUrl: '', token: '', quality: '', savePath: '' };

  // ---------------------------------------------------------------- 工具

  function showMsg(node, text, kind) {
    if (!node) return;
    node.textContent = text;
    node.className = 'msg' + (kind ? ' ' + kind : '');
    node.hidden = !text;
  }

  function formatTime(ms) {
    if (!ms) return '';
    var d = new Date(ms);
    var p = function (n) { return (n < 10 ? '0' : '') + n; };
    return d.getFullYear() + '-' + p(d.getMonth() + 1) + '-' + p(d.getDate()) +
      ' ' + p(d.getHours()) + ':' + p(d.getMinutes());
  }

  function formatSize(bytes) {
    if (!bytes) return '';
    var units = ['B', 'KB', 'MB', 'GB', 'TB'];
    var v = bytes;
    var i = 0;
    while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
    return (i === 0 ? v : v.toFixed(1)) + ' ' + units[i];
  }

  // ---------------------------------------------------------------- 当前页

  function loadCurrentTab() {
    // 弹窗作为浏览器动作打开时，currentWindow 指向宿主窗口；lastFocusedWindow 作兜底。
    return queryActiveTab().then(function (tab) {
      currentTab = tab || null;
      var url = currentTab ? currentTab.url : '';
      if (!url) {
        el.pageUrl.textContent = '无法读取当前标签页地址（浏览器内部页或未授权）';
        el.btnSubmit.disabled = true;
        return;
      }
      el.pageUrl.textContent = url;
      el.pageUrl.title = url;
      el.btnSubmit.disabled = !isHttpUrl(url);
    });
  }

  function queryActiveTab() {
    return chrome.tabs.query({ active: true, currentWindow: true }).then(function (tabs) {
      if (tabs && tabs.length && tabs[0].url) return tabs[0];
      return chrome.tabs.query({ active: true, lastFocusedWindow: true }).then(function (tabs2) {
        return (tabs2 && tabs2[0]) || (tabs && tabs[0]) || null;
      });
    }).catch(function () {
      return null;
    });
  }

  function isHttpUrl(url) {
    return /^https?:\/\//i.test(String(url || ''));
  }

  // ---------------------------------------------------------------- 提交

  function submitCurrentPage() {
    if (!currentTab) return;
    el.btnSubmit.disabled = true;
    showMsg(el.submitMsg, '提交中…');
    chrome.runtime.sendMessage({
      type: 'raccoon:submit-page',
      tabId: currentTab.id,
      pageUrl: currentTab.url
    }, function (resp) {
      el.btnSubmit.disabled = false;
      if (chrome.runtime.lastError) {
        showMsg(el.submitMsg, '提交失败：' + chrome.runtime.lastError.message, 'err');
        return;
      }
      if (!resp || !resp.ok) {
        showMsg(el.submitMsg, '提交失败：' + ((resp && resp.error) || '未知错误'), 'err');
        return;
      }
      var created = (resp.result && resp.result.created) || [];
      var conflicts = (resp.result && resp.result.conflicts) || [];
      var lines = ['已提交 ' + created.length + ' 个任务。'];
      if (created.length) {
        lines.push('任务 ID：' + created.map(function (t) { return t.id; }).join('、'));
      }
      if (conflicts.length) {
        lines.push('跳过 ' + conflicts.length + ' 个重复链接（队列中已存在）。');
      }
      showMsg(el.submitMsg, lines.join('\n'), 'ok');
      loadTasks();
    });
  }

  // ---------------------------------------------------------------- 任务列表

  function loadTasks() {
    if (!config.baseUrl || !config.token) {
      el.taskList.innerHTML = '';
      var li = document.createElement('li');
      li.className = 'empty';
      li.textContent = '请先在下方配置实例地址与 API Token。';
      el.taskList.appendChild(li);
      return Promise.resolve();
    }
    var client;
    try {
      client = RaccoonAPI.createClient({ baseUrl: config.baseUrl, token: config.token });
    } catch (e) {
      showMsg(el.taskMsg, RaccoonAPI.describeError(e), 'err');
      return Promise.resolve();
    }
    var status = el.statusFilter.value || '';
    return client.listTasks({ status: status, limit: 20 }).then(function (res) {
      renderTasks(res.items || []);
    }).catch(function (err) {
      el.taskList.innerHTML = '';
      var li = document.createElement('li');
      li.className = 'empty';
      li.textContent = '任务列表加载失败';
      el.taskList.appendChild(li);
      showMsg(el.taskMsg, RaccoonAPI.describeError(err), 'err');
    });
  }

  function renderTasks(items) {
    el.taskList.innerHTML = '';
    if (!items.length) {
      var li = document.createElement('li');
      li.className = 'empty';
      li.textContent = '没有任务。';
      el.taskList.appendChild(li);
      return;
    }
    items.forEach(function (t) {
      var li = document.createElement('li');
      li.className = 'task-item';

      var main = document.createElement('div');
      main.className = 'task-main';

      var title = document.createElement('span');
      title.className = 'task-title';
      title.textContent = t.title || t.url || t.id;
      title.title = t.url || t.id;

      var meta = document.createElement('span');
      meta.className = 'task-meta';
      var bits = [];
      if (t.platform) bits.push(t.platform);
      if (t.quality) bits.push(t.quality);
      if (t.expected_size) bits.push(formatSize(t.expected_size));
      if (t.created_at) bits.push(formatTime(t.created_at));
      if (t.error_message) bits.push(t.error_message);
      meta.textContent = bits.join(' · ');

      main.appendChild(title);
      main.appendChild(meta);

      var badge = document.createElement('span');
      badge.className = 'badge ' + (t.status || '');
      badge.textContent = STATUS_TEXT[t.status] || t.status || '未知';

      li.appendChild(main);
      li.appendChild(badge);
      el.taskList.appendChild(li);
    });
  }

  // ---------------------------------------------------------------- 配置

  function fillConfigForm() {
    el.cfgBaseUrl.value = config.baseUrl;
    el.cfgToken.value = config.token;
    el.cfgQuality.value = config.quality;
    el.cfgSavePath.value = config.savePath;
    el.instanceLabel.textContent = config.baseUrl
      ? config.baseUrl
      : '未配置实例（点下方「配置」填写）';
  }

  function saveConfig() {
    var baseUrl = el.cfgBaseUrl.value.trim();
    var token = el.cfgToken.value.trim();
    if (!baseUrl) {
      showMsg(el.cfgMsg, '请填写实例地址。', 'err');
      return;
    }
    if (!token) {
      showMsg(el.cfgMsg, '请填写 API Token。', 'err');
      return;
    }
    var normalized;
    try {
      normalized = RaccoonAPI.normalizeBaseUrl(baseUrl);
    } catch (e) {
      showMsg(el.cfgMsg, RaccoonAPI.describeError(e), 'err');
      return;
    }
    // 实例地址是用户自填的，manifest 静态 host_permissions 覆盖不到，
    // 必须在保存时按需申请该源的站点权限。chrome.permissions.request 需在用户手势
    // 内调用，因此这里同步发起（已授权时静默返回 true，不重复弹窗）。
    var pattern;
    try {
      pattern = RaccoonAPI.originPattern(normalized);
    } catch (e) {
      showMsg(el.cfgMsg, RaccoonAPI.describeError(e), 'err');
      return;
    }
    if (!chrome.permissions || !chrome.permissions.request) {
      persistConfig(normalized, token);
      return;
    }
    chrome.permissions.request({ origins: [pattern] }, function (granted) {
      if (!granted) {
        showMsg(el.cfgMsg, '未获得该实例地址的站点权限，扩展无法访问它。请允许后重试。', 'warn');
        return;
      }
      persistConfig(normalized, token);
    });
  }

  /**
   * 写入配置并刷新界面。
   * @param {string} normalized
   * @param {string} token
   */
  function persistConfig(normalized, token) {
    RaccoonSettings.save({
      baseUrl: normalized,
      token: token,
      quality: el.cfgQuality.value.trim(),
      savePath: el.cfgSavePath.value.trim()
    }).then(function (next) {
      config = next;
      fillConfigForm();
      showMsg(el.cfgMsg, '已保存。Token 只存本机 chrome.storage.local。', 'ok');
      return loadTasks();
    }).catch(function (err) {
      showMsg(el.cfgMsg, '保存失败：' + RaccoonAPI.describeError(err), 'err');
    });
  }

  function testConnection() {
    var baseUrl = el.cfgBaseUrl.value.trim() || config.baseUrl;
    var token = el.cfgToken.value.trim() || config.token;
    if (!baseUrl || !token) {
      showMsg(el.cfgMsg, '先填实例地址与 API Token。', 'err');
      return;
    }
    var client;
    try {
      client = RaccoonAPI.createClient({ baseUrl: baseUrl, token: token });
    } catch (e) {
      showMsg(el.cfgMsg, RaccoonAPI.describeError(e), 'err');
      return;
    }
    el.btnTest.disabled = true;
    showMsg(el.cfgMsg, '测试中…');
    // 用最小代价的读接口验证：GET /api/v1/tasks?limit=1
    client.listTasks({ limit: 1 }).then(function (res) {
      showMsg(el.cfgMsg, '连接正常，实例返回 ' + (res.total != null ? res.total : 0) + ' 个任务（累计）。', 'ok');
    }).catch(function (err) {
      showMsg(el.cfgMsg, '连接失败：' + RaccoonAPI.describeError(err), 'err');
    }).then(function () {
      el.btnTest.disabled = false;
    });
  }

  // ---------------------------------------------------------------- 事件绑定

  el.btnSubmit.addEventListener('click', submitCurrentPage);
  el.btnRefreshPage.addEventListener('click', loadCurrentTab);
  el.btnReload.addEventListener('click', loadTasks);
  el.statusFilter.addEventListener('change', loadTasks);
  el.btnSave.addEventListener('click', saveConfig);
  el.btnTest.addEventListener('click', testConnection);

  el.btnToggleConfig.addEventListener('click', function () {
    var collapsed = el.configBody.hidden;
    el.configBody.hidden = !collapsed;
    el.btnToggleConfig.textContent = collapsed ? '收起' : '展开';
    el.btnToggleConfig.setAttribute('aria-expanded', String(collapsed));
  });

  el.linkDocs.addEventListener('click', function (ev) {
    ev.preventDefault();
    chrome.tabs.create({ url: chrome.runtime.getURL('README.md') });
  });

  // ---------------------------------------------------------------- 初始化

  RaccoonSettings.load().then(function (cfg) {
    config = cfg;
    fillConfigForm();
    return loadCurrentTab();
  }).then(function () {
    return loadTasks();
  }).catch(function (err) {
    showMsg(el.cfgMsg, '初始化失败：' + RaccoonAPI.describeError(err), 'err');
  });
})();
