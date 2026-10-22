/*!
 * background.js — Raccoon 扩展 Service Worker（MV3）
 *
 * 职责（docs/01 §3.6「浏览器扩展 / iOS 快捷指令 → 提交链接 / 查询状态」）：
 *   1. 注册右键菜单「用 Raccoon 下载」；
 *   2. 捕获页面里的媒体链接（chrome.scripting 注入，只读 DOM）；
 *   3. 调用 Raccoon HTTP API（POST /api/v1/tasks，docs/04 §2.2）；
 *   4. 结果用 chrome.notifications 通知。
 *
 * 与 popup 的分工：提交链路统一走 background（保证一次点击只产生一批任务），
 * popup 只发消息与展示；纯读操作（任务列表）由 popup 直接调 GET /api/v1/tasks。
 *
 * 经典脚本（非 ES module）：与 api.js / settings.js 通过 importScripts 共享全局对象。
 */
importScripts('/src/api.js');
importScripts('/src/settings.js');

/** 右键菜单项 ID。 */
var MENU_DOWNLOAD_LINK = 'raccoon-download-link';
var MENU_DOWNLOAD_PAGE = 'raccoon-download-page';
var MENU_DOWNLOAD_SELECTION = 'raccoon-download-selection';

/** 单次捕获的媒体链接上限（避免一次点击灌爆队列）。 */
var MAX_CAPTURED_URLS = 10;

/** 捕获到的媒体链接在菜单标题上的回显上限。 */
var MENU_TITLE_MAX = 40;

/**
 * 安装 / 更新时重建右键菜单。
 */
chrome.runtime.onInstalled.addListener(function () {
  rebuildMenus();
});

/** 浏览器启动时也确保菜单存在（MV3 SW 会休眠，菜单需持久化）。 */
chrome.runtime.onStartup.addListener(function () {
  rebuildMenus();
});

function rebuildMenus() {
  chrome.contextMenus.removeAll(function () {
    chrome.contextMenus.create({
      id: MENU_DOWNLOAD_LINK,
      title: '用 Raccoon 下载',
      contexts: ['link']
    });
    chrome.contextMenus.create({
      id: MENU_DOWNLOAD_PAGE,
      title: '用 Raccoon 下载此页（含媒体直链）',
      contexts: ['page']
    });
    chrome.contextMenus.create({
      id: MENU_DOWNLOAD_SELECTION,
      title: '用 Raccoon 下载选中的链接',
      contexts: ['selection']
    });
  });
}

/**
 * 右键菜单点击处理。
 */
chrome.contextMenus.onClicked.addListener(function (info, tab) {
  var itemId = info.menuItemId;
  if (itemId !== MENU_DOWNLOAD_LINK && itemId !== MENU_DOWNLOAD_PAGE && itemId !== MENU_DOWNLOAD_SELECTION) {
    return;
  }
  handleMenuClick(itemId, info, tab);
});

function handleMenuClick(itemId, info, tab) {
  if (itemId === MENU_DOWNLOAD_LINK) {
    if (!info.linkUrl) {
      notify('Raccoon', '没有取到链接地址');
      return;
    }
    submitAndNotify([info.linkUrl], '右键链接');
    return;
  }
  if (itemId === MENU_DOWNLOAD_SELECTION) {
    var picked = pickUrlFromText(info.selectionText || '');
    if (!picked) {
      notify('Raccoon', '选中的文本里没有找到链接');
      return;
    }
    submitAndNotify([picked], '选中文本');
    return;
  }
  // MENU_DOWNLOAD_PAGE：先抓页面里的媒体直链，抓不到就退化为提交页面地址。
  var pageUrl = (tab && tab.url) || info.pageUrl || '';
  if (!pageUrl) {
    notify('Raccoon', '没有取到页面地址');
    return;
  }
  capturePageLinks(tab, pageUrl).then(function (captured) {
    var urls = captured.length ? captured.slice(0, MAX_CAPTURED_URLS) : [pageUrl];
    submitAndNotify(urls, captured.length ? '页面媒体链接' : '页面地址');
  });
}

/**
 * 注入脚本采集页面中的媒体直链与规范链接（只读 DOM，不修改页面）。
 * @param {object} tab
 * @param {string} pageUrl
 * @returns {Promise<string[]>}
 */
function capturePageLinks(tab, pageUrl) {
  if (!tab || typeof tab.id !== 'number') return Promise.resolve([]);
  return chrome.scripting.executeScript({
    target: { tabId: tab.id },
    func: function () {
      var found = [];
      var push = function (u) {
        if (!u) return;
        if (u.indexOf('http://') !== 0 && u.indexOf('https://') !== 0) return;
        if (found.indexOf(u) === -1) found.push(u);
      };
      var mediaTags = document.querySelectorAll('video, audio, source, embed');
      for (var i = 0; i < mediaTags.length; i++) {
        var el = mediaTags[i];
        push(el.currentSrc || el.src || el.getAttribute('src'));
        if (el.tagName === 'VIDEO' || el.tagName === 'AUDIO') {
          var srcs = el.querySelectorAll('source');
          for (var j = 0; j < srcs.length; j++) {
            push(srcs[j].src || srcs[j].getAttribute('src'));
          }
        }
      }
      var metas = document.querySelectorAll('meta[property], meta[name]');
      for (var k = 0; k < metas.length; k++) {
        var key = (metas[k].getAttribute('property') || metas[k].getAttribute('name') || '').toLowerCase();
        if (key === 'og:video' || key === 'og:video:url' || key === 'twitter:player:stream') {
          push(metas[k].getAttribute('content'));
        }
      }
      var canonical = document.querySelector('link[rel="canonical"]');
      if (canonical) push(canonical.href);
      return found;
    }
  }).then(function (results) {
    var out = [];
    if (results && results.length && results[0] && Array.isArray(results[0].result)) {
      out = results[0].result;
    }
    // 页面自身的地址放在最后（作为兜底项）。
    if (pageUrl && out.indexOf(pageUrl) === -1) out.push(pageUrl);
    return out;
  }).catch(function () {
    return [pageUrl];
  });
}

/**
 * 从任意文本里挑出第一条 http(s) 链接。
 * @param {string} text
 * @returns {string}
 */
function pickUrlFromText(text) {
  var m = String(text || '').match(/https?:\/\/[^\s"'<>）)】]+/i);
  return m ? m[0] : '';
}

/**
 * 读取配置并构造 API 客户端。
 * @returns {Promise<object>}
 */
function buildClient() {
  return RaccoonSettings.load().then(function (cfg) {
    if (!cfg.baseUrl) throw new Error('未配置 Raccoon 实例地址，请打开扩展弹窗填写');
    if (!cfg.token) throw new Error('未配置 API Token，请打开扩展弹窗填写');
    return RaccoonAPI.createClient({ baseUrl: cfg.baseUrl, token: cfg.token });
  });
}

/**
 * 提交任务并弹通知。
 * @param {string[]} urls
 * @param {string} origin 来源描述（右键链接 / 页面地址 …）
 */
function submitAndNotify(urls, origin) {
  var list = (urls || []).filter(function (u) { return !!u; });
  if (!list.length) {
    notify('Raccoon', '没有可提交的链接');
    return Promise.resolve(null);
  }
  var start = Date.now();
  return buildClient().then(function (client) {
    return RaccoonSettings.load().then(function (cfg) {
      return client.submitTasks(
        list.map(function (u) {
          return { url: u, quality: cfg.quality || '', savePath: cfg.savePath || '' };
        })
      );
    });
  }).then(function (result) {
    var created = result.created || [];
    var conflicts = result.conflicts || [];
    var lines = [];
    lines.push('已提交 ' + created.length + ' 个任务（' + origin + '，' + Math.round((Date.now() - start) / 100) / 10 + 's）');
    if (created.length) {
      lines.push('任务 ID：' + created.map(function (t) { return t.id; }).join('、'));
    }
    if (conflicts.length) {
      var ids = conflicts.map(function (c) { return c.existing_task_id; }).filter(function (x) { return !!x; });
      lines.push(ids.length
        ? '跳过 ' + conflicts.length + ' 个重复链接（已在队列：' + ids.join('、') + '）'
        : '跳过 ' + conflicts.length + ' 个重复链接（已在队列中）');
    }
    notify('Raccoon · 提交完成', lines.join('\n'));
    return result;
  }).catch(function (err) {
    var msg = RaccoonAPI.describeError(err);
    if (err && err.code === 'CONFLICT') {
      msg += '（该链接已在队列中）';
    }
    notify('Raccoon · 提交失败', msg);
    throw err;
  });
}

/**
 * 系统通知。
 * @param {string} title
 * @param {string} message
 */
function notify(title, message) {
  var id = 'raccoon-' + Date.now();
  var text = String(message || '');
  if (text.length > 400) text = text.slice(0, 400) + '…';
  try {
    chrome.notifications.create(id, {
      type: 'basic',
      iconUrl: chrome.runtime.getURL('icons/icon128.png'),
      title: String(title || 'Raccoon'),
      message: text,
      priority: 0
    });
  } catch (e) {
    // 通知失败不应影响主流程。
    console.warn('[raccoon] 通知失败', e);
  }
}

/**
 * popup → background 消息通道。
 */
chrome.runtime.onMessage.addListener(function (msg, sender, sendResponse) {
  if (!msg || typeof msg !== 'object') return false;

  if (msg.type === 'raccoon:submit-page') {
    var tabId = msg.tabId;
    var pageUrl = msg.pageUrl || '';
    var flow = Promise.resolve({ urls: [] });
    if (tabId !== undefined && tabId !== null) {
      flow = chrome.tabs.get(tabId).then(function (tab) {
        return capturePageLinks(tab, pageUrl || (tab && tab.url) || '');
      }).catch(function () {
        return pageUrl ? [pageUrl] : [];
      });
    }
    flow.then(function (urls) {
      var final = (urls && urls.length ? urls : (pageUrl ? [pageUrl] : [])).slice(0, MAX_CAPTURED_URLS);
      if (!final.length) throw new Error('没有取到可提交的链接');
      return submitAndNotify(final, '弹窗');
    }).then(function (result) {
      sendResponse({ ok: true, result: result });
    }).catch(function (err) {
      sendResponse({ ok: false, error: RaccoonAPI.describeError(err) });
    });
    return true;
  }

  if (msg.type === 'raccoon:submit-urls') {
    submitAndNotify(msg.urls || [], '弹窗').then(function (result) {
      sendResponse({ ok: true, result: result });
    }).catch(function (err) {
      sendResponse({ ok: false, error: RaccoonAPI.describeError(err) });
    });
    return true;
  }

  return false;
});

/** 供调试：service worker 内可直接读配置。 */
self.RaccoonBackground = {
  capturePageLinks: capturePageLinks,
  submitAndNotify: submitAndNotify,
  notify: notify
};
