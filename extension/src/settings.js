/*!
 * settings.js — 扩展配置存取（chrome.storage.local）
 *
 * 隐私铁律：API Token 只写 chrome.storage.local，不进 sync、不发网络、不进日志。
 * 见 extension/README.md「隐私说明」。
 */
(function (global) {
  'use strict';

  /** 配置在 chrome.storage.local 里的键。 */
  var STORAGE_KEY = 'raccoon.settings';

  /** 配置默认值。 */
  var DEFAULTS = {
    /** Raccoon 实例地址，如 http://127.0.0.1:8530 */
    baseUrl: '',
    /** API Token（Bearer Token 通道） */
    token: '',
    /** 默认下载质量，空字符串表示用服务端默认（download.default_quality） */
    quality: '',
    /** 默认保存目录（对应 save_path），空字符串表示用服务端命名规则 */
    savePath: ''
  };

  /**
   * 读取配置（缺失字段用默认值补齐）。
   * @returns {Promise<{baseUrl:string, token:string, quality:string, savePath:string}>}
   */
  function load() {
    return chrome.storage.local.get(STORAGE_KEY).then(function (got) {
      var raw = (got && got[STORAGE_KEY]) || {};
      return {
        baseUrl: typeof raw.baseUrl === 'string' ? raw.baseUrl : DEFAULTS.baseUrl,
        token: typeof raw.token === 'string' ? raw.token : DEFAULTS.token,
        quality: typeof raw.quality === 'string' ? raw.quality : DEFAULTS.quality,
        savePath: typeof raw.savePath === 'string' ? raw.savePath : DEFAULTS.savePath
      };
    });
  }

  /**
   * 保存配置（只覆盖传入的字段）。
   * @param {object} patch
   * @returns {Promise<object>} 保存后的完整配置
   */
  function save(patch) {
    return load().then(function (cur) {
      var next = {
        baseUrl: patch && typeof patch.baseUrl === 'string' ? patch.baseUrl.trim() : cur.baseUrl,
        token: patch && typeof patch.token === 'string' ? patch.token.trim() : cur.token,
        quality: patch && typeof patch.quality === 'string' ? patch.quality.trim() : cur.quality,
        savePath: patch && typeof patch.savePath === 'string' ? patch.savePath.trim() : cur.savePath
      };
      var payload = {};
      payload[STORAGE_KEY] = next;
      return chrome.storage.local.set(payload).then(function () {
        return next;
      });
    });
  }

  /** 清除全部配置（含 Token）。 */
  function clear() {
    return chrome.storage.local.remove(STORAGE_KEY);
  }

  global.RaccoonSettings = {
    STORAGE_KEY: STORAGE_KEY,
    DEFAULTS: DEFAULTS,
    load: load,
    save: save,
    clear: clear
  };
})(typeof globalThis !== 'undefined' ? globalThis : self);
