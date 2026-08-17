/* ============================================================
   app.js — 共享交互（所有页面加载）
   仅含跨页通用行为，禁止页面专属逻辑
   Modal 约定：backdrop id="modal-x"，.modal 为其子元素
   Drawer 约定：面板 id="drawer-x"，背景 id="drawer-x-backdrop"
   ============================================================ */
(function () {
  'use strict';

  var FOCUSABLE_SELECTOR = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

  /* ---------- Toast ---------- */
  window.showToast = function (message, type) {
    var container = document.querySelector('.toast-container');
    if (!container) {
      container = document.createElement('div');
      container.className = 'toast-container';
      container.setAttribute('aria-live', 'polite');
      document.body.appendChild(container);
    }
    var toast = document.createElement('div');
    toast.className = 'toast ' + (type || '');
    var icon = type === 'success' ? '✓' : type === 'error' ? '✗' : type === 'warning' ? '⚠' : 'ℹ';
    toast.innerHTML = '<span aria-hidden="true">' + icon + '</span><span>' + message + '</span>';
    container.appendChild(toast);
    setTimeout(function () { toast.remove(); }, 3000);
  };

  /* ---------- Focus trap helpers (Modal / Drawer 共用) ---------- */
  var activeRelease = null;

  function trapFocus(container, previouslyFocused) {
    var focusables = container.querySelectorAll(FOCUSABLE_SELECTOR);
    if (focusables.length > 0) focusables[0].focus();

    function onKeydown(e) {
      if (e.key !== 'Tab') return;
      var items = container.querySelectorAll(FOCUSABLE_SELECTOR);
      if (items.length === 0) return;
      var first = items[0];
      var last = items[items.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault(); last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault(); first.focus();
      }
    }
    container.addEventListener('keydown', onKeydown);
    return function release() {
      container.removeEventListener('keydown', onKeydown);
      if (previouslyFocused && previouslyFocused.focus) previouslyFocused.focus();
    };
  }

  /* ---------- Modal / Drawer open & close (generic) ---------- */
  function openLayer(id, triggerEl) {
    var backdrop = document.getElementById(id);
    var layer = null;
    if (backdrop && backdrop.classList.contains('modal-backdrop')) {
      layer = backdrop.querySelector('.modal');
    } else {
      layer = backdrop; /* id 即面板 id */
      backdrop = document.getElementById(id + '-backdrop');
    }
    if (!backdrop || !layer) return;
    backdrop.classList.add('open');
    backdrop.setAttribute('aria-hidden', 'false');
    layer.classList.add('open');
    if (activeRelease) activeRelease();
    activeRelease = trapFocus(layer, triggerEl);
  }

  function closeBackdrop(backdrop) {
    backdrop.classList.remove('open');
    backdrop.setAttribute('aria-hidden', 'true');
    var panelId = backdrop.id.replace(/-backdrop$/, '');
    if (panelId !== backdrop.id) {
      var panel = document.getElementById(panelId);
      if (panel) panel.classList.remove('open');
    }
    if (activeRelease) { activeRelease(); activeRelease = null; }
  }

  window.openModal = function (id, trigger) { openLayer(id, trigger); };
  window.closeModal = function (id) {
    var backdrop = document.getElementById(id);
    if (backdrop) closeBackdrop(backdrop);
  };
  window.openDrawer = function (id, trigger) { openLayer(id, trigger); };
  window.closeDrawer = function (id) {
    var panel = document.getElementById(id);
    var backdrop = panel ? document.getElementById(id + '-backdrop') : null;
    if (backdrop) closeBackdrop(backdrop);
  };

  /* ---------- Escape 关闭最上层 Modal/Drawer ---------- */
  document.addEventListener('keydown', function (e) {
    if (e.key !== 'Escape') return;
    var openBackdrops = document.querySelectorAll('.modal-backdrop.open, .drawer-backdrop.open');
    if (openBackdrops.length === 0) return;
    var last = openBackdrops[openBackdrops.length - 1];
    if (last.getAttribute('data-lock') === 'true') return; // 处理中不可关闭
    closeBackdrop(last);
  });

  /* ---------- 声明式触发绑定 ----------
     [data-open-modal="id"]  / [data-close-modal]
     [data-open-drawer="id"] / [data-close-drawer]
     [data-toggle-dropdown]  / 自动 click-outside 关闭
     [data-tab] + [data-tab-group] + [data-tab-panel]
     [data-accordion]
     [data-state-switch="viewId"]  状态视图切换（loading/empty/error/populated）
     [data-copy] 复制文本
  ------------------------------------------------------------ */
  document.addEventListener('click', function (e) {
    var t = e.target.closest('[data-open-modal],[data-close-modal],[data-open-drawer],[data-close-drawer],[data-toggle-dropdown],[data-accordion],[data-state-switch],[data-copy]');
    if (!t) return;

    if (t.hasAttribute('data-open-modal')) {
      e.preventDefault();
      openLayer(t.getAttribute('data-open-modal'), t);
    } else if (t.hasAttribute('data-close-modal')) {
      e.preventDefault();
      var mb = t.closest('.modal-backdrop');
      if (mb) closeBackdrop(mb);
    } else if (t.hasAttribute('data-open-drawer')) {
      e.preventDefault();
      openLayer(t.getAttribute('data-open-drawer'), t);
    } else if (t.hasAttribute('data-close-drawer')) {
      e.preventDefault();
      var dp = t.closest('.drawer');
      if (dp) closeBackdrop(document.getElementById(dp.id + '-backdrop'));
    } else if (t.hasAttribute('data-toggle-dropdown')) {
      e.preventDefault();
      var dd = t.closest('.dropdown');
      document.querySelectorAll('.dropdown.open').forEach(function (d) { if (d !== dd) d.classList.remove('open'); });
      if (dd) dd.classList.toggle('open');
    } else if (t.hasAttribute('data-accordion')) {
      e.preventDefault();
      var item = t.closest('.accordion-item');
      if (item) {
        item.classList.toggle('open');
        t.setAttribute('aria-expanded', item.classList.contains('open') ? 'true' : 'false');
      }
    } else if (t.hasAttribute('data-state-switch')) {
      e.preventDefault();
      switchState(t.getAttribute('data-state-switch'));
      document.querySelectorAll('.state-switcher button').forEach(function (b) {
        b.classList.toggle('active', b === t);
      });
    } else if (t.hasAttribute('data-copy')) {
      e.preventDefault();
      copyText(t.getAttribute('data-copy') || t.textContent.trim());
      showToast('已复制', 'success');
    }
  });

  /* 下拉菜单 click-outside 关闭 */
  document.addEventListener('click', function (e) {
    if (!e.target.closest('.dropdown')) {
      document.querySelectorAll('.dropdown.open').forEach(function (d) { d.classList.remove('open'); });
    }
  });

  /* 背景点击关闭（data-lock 保护中的除外） */
  document.addEventListener('click', function (e) {
    var backdrop = e.target.closest('.modal-backdrop.open, .drawer-backdrop.open');
    if (!backdrop || e.target !== backdrop) return;
    if (backdrop.getAttribute('data-lock') === 'true') return;
    closeBackdrop(backdrop);
  });

  /* ---------- Tabs（通用） ---------- */
  document.addEventListener('click', function (e) {
    var tab = e.target.closest('[data-tab]');
    if (!tab) return;
    e.preventDefault();
    var group = tab.getAttribute('data-tab-group');
    var target = tab.getAttribute('data-tab');
    document.querySelectorAll('[data-tab][data-tab-group="' + group + '"]').forEach(function (el) {
      el.classList.toggle('active', el === tab);
      el.setAttribute('aria-selected', el === tab ? 'true' : 'false');
    });
    document.querySelectorAll('[data-tab-panel][data-tab-group="' + group + '"]').forEach(function (panel) {
      var show = panel.getAttribute('data-tab-panel') === target;
      panel.classList.toggle('hidden', !show);
    });
  });

  /* ---------- 状态视图切换（loading / empty / error / populated） ---------- */
  window.switchState = function (viewId) {
    var target = document.getElementById(viewId);
    if (!target) return;
    var container = target.closest('[data-state-container]');
    if (!container) return;
    container.querySelectorAll('.state-view').forEach(function (v) { v.classList.remove('active'); });
    target.classList.add('active');
  };

  /* ---------- 复制到剪贴板 ---------- */
  window.copyText = function (text) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).catch(function () { fallbackCopy(text); });
    } else {
      fallbackCopy(text);
    }
  };
  function fallbackCopy(text) {
    var ta = document.createElement('textarea');
    ta.value = text; document.body.appendChild(ta); ta.select();
    try { document.execCommand('copy'); } catch (err) { /* noop */ }
    ta.remove();
  }

  /* ---------- 移动端导航抽屉 ---------- */
  var navToggle = document.querySelector('.nav-toggle');
  var navDrawer = document.getElementById('nav-drawer');
  var navBackdrop = document.getElementById('nav-backdrop');
  if (navToggle && navDrawer) {
    navToggle.addEventListener('click', function () {
      navDrawer.classList.add('open');
      if (navBackdrop) navBackdrop.classList.add('open');
    });
    if (navBackdrop) {
      navBackdrop.addEventListener('click', function () {
        navDrawer.classList.remove('open');
        navBackdrop.classList.remove('open');
      });
    }
  }

  /* ---------- 只读角色路由拦截横幅（共享） ----------
     index.html 角色切换写入 localStorage('cam-role')；
     带 data-ro-intercept 的页面在只读角色下显示无权限横幅。 */
  function renderRoleBanner() {
    if (document.body.getAttribute('data-ro-intercept') !== 'true') return;
    if (window.localStorage.getItem('cam-role') !== 'readonly') return;
    if (document.querySelector('.ro-banner')) return;
    var banner = document.createElement('div');
    banner.className = 'banner banner-error ro-banner';
    banner.setAttribute('role', 'alert');
    banner.innerHTML = '<span class="banner-icon" aria-hidden="true">⛔</span>' +
      '<div><div class="banner-title">无权限访问，仅到期看板可见</div>' +
      '<div class="banner-body">当前角色为只读查看者，本页面已被前端拦截（生产环境由 EIAM 同步拦截）。可前往到期看板查看证书到期与探测状态。</div></div>' +
      '<div class="banner-actions"><a class="btn btn-primary btn-sm" href="dashboard.html">前往到期看板</a></div>';
    var main = document.getElementById('main');
    if (main) main.prepend(banner);
  }
  renderRoleBanner();
})();
