/* filemanager-web single-page UI */
(function () {
  'use strict';

  var api = new ApiClient();
  var state = { path: '', entries: [], selected: {}, info: null };

  // ---- tiny DOM helpers --------------------------------------------------
  function el(id) { return document.getElementById(id); }
  function h(tag, attrs, children) {
    var node = document.createElement(tag);
    attrs = attrs || {};
    for (var k in attrs) {
      if (k === 'class') node.className = attrs[k];
      else if (k === 'text') node.textContent = attrs[k];
      else if (k === 'html') node.innerHTML = attrs[k];
      else if (k.indexOf('on') === 0) node.addEventListener(k.substr(2), attrs[k]);
      else node.setAttribute(k, attrs[k]);
    }
    (children || []).forEach(function (c) {
      if (c == null) return;
      node.appendChild(typeof c === 'string' ? document.createTextNode(c) : c);
    });
    return node;
  }
  function show(node) { node.classList.remove('hidden'); }
  function hide(node) { node.classList.add('hidden'); }

  function toast(msg, kind) {
    var t = h('div', { class: 'toast ' + (kind || 'info'), text: msg });
    el('toasts').appendChild(t);
    setTimeout(function () { t.classList.add('show'); }, 10);
    setTimeout(function () {
      t.classList.remove('show');
      setTimeout(function () { t.remove(); }, 300);
    }, 3500);
  }

  function humanSize(n) {
    if (n === 0) return '0 B';
    var u = ['B', 'KB', 'MB', 'GB', 'TB'], i = 0;
    while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
    return (i === 0 ? n : n.toFixed(1)) + ' ' + u[i];
  }

  function iconFor(entry) {
    if (entry.is_dir) return '📁';
    var ext = (entry.name.split('.').pop() || '').toLowerCase();
    if (['png','jpg','jpeg','gif','webp','svg','bmp','ico'].indexOf(ext) >= 0) return '🖼️';
    if (['mp3','wav','ogg','flac','m4a'].indexOf(ext) >= 0) return '🎵';
    if (['mp4','mkv','webm','mov','avi'].indexOf(ext) >= 0) return '🎬';
    if (['zip','tar','gz','tgz','rar','7z'].indexOf(ext) >= 0) return '🗜️';
    if (['pdf'].indexOf(ext) >= 0) return '📕';
    return '📄';
  }

  var TEXT_EXT = ['txt','md','markdown','js','ts','json','css','html','htm','xml',
    'yml','yaml','ini','cfg','conf','sh','bash','py','rb','php','go','rs','c','h',
    'cpp','hpp','java','sql','log','csv','toml','env','gitignore','dockerfile'];
  function isTextFile(entry) {
    var ext = (entry.name.split('.').pop() || '').toLowerCase();
    return TEXT_EXT.indexOf(ext) >= 0;
  }

  // ---- navigation / listing ---------------------------------------------
  async function loadDir(path) {
    try {
      var res = await api.call('list', { path: path });
      state.path = res.path;
      state.entries = res.entries;
      state.selected = {};
      render(res);
    } catch (e) {
      toast(e.message || 'Failed to list', 'error');
    }
  }

  function renderBreadcrumb() {
    var bc = el('breadcrumb');
    bc.innerHTML = '';
    bc.appendChild(h('a', {
      class: 'crumb', text: '🏠', title: 'root',
      onclick: function () { loadDir(''); }
    }));
    var parts = state.path ? state.path.split('/') : [];
    var acc = '';
    parts.forEach(function (p) {
      acc = acc ? acc + '/' + p : p;
      var target = acc;
      bc.appendChild(h('span', { class: 'sep', text: '/' }));
      bc.appendChild(h('a', {
        class: 'crumb', text: p,
        onclick: function () { loadDir(target); }
      }));
    });
  }

  function render(res) {
    renderBreadcrumb();
    var tbody = el('filelist');
    tbody.innerHTML = '';

    if (state.path) {
      var parent = state.path.split('/').slice(0, -1).join('/');
      tbody.appendChild(h('tr', { class: 'row up' }, [
        h('td', {}, []),
        h('td', { class: 'name', onclick: function () { loadDir(parent); } },
          [h('span', { class: 'ic', text: '↩️' }), h('span', { text: '..' })]),
        h('td', {}, []), h('td', {}, []), h('td', {}, []), h('td', {}, [])
      ]));
    }

    state.entries.forEach(function (entry) {
      var cb = h('input', { type: 'checkbox', class: 'sel',
        onchange: function () {
          if (cb.checked) state.selected[entry.path] = entry;
          else delete state.selected[entry.path];
          updateBatchBar();
        } });

      var nameCell = h('td', { class: 'name' }, [
        h('span', { class: 'ic', text: iconFor(entry) }),
        h('span', {
          class: 'label' + (entry.is_dir ? ' dir' : ''),
          text: entry.name,
          onclick: function () {
            if (entry.is_dir) loadDir(entry.path);
            else if (isTextFile(entry)) openEditor(entry);
            else doDownload(entry);
          }
        })
      ]);

      var actions = h('td', { class: 'actions' }, [
        !entry.is_dir ? actBtn('⬇', 'Download', function () { doDownload(entry); }) : null,
        !entry.is_dir && isTextFile(entry) ? actBtn('✏', 'Edit', function () { openEditor(entry); }) : null,
        canWrite() ? actBtn('🏷', 'Rename', function () { doRename(entry); }) : null,
        canWrite() ? actBtn('🗑', 'Delete', function () { doDelete([entry]); }) : null
      ]);

      tbody.appendChild(h('tr', { class: 'row' }, [
        h('td', { class: 'cbcol' }, [cb]),
        nameCell,
        h('td', { class: 'size', text: entry.is_dir ? '—' : humanSize(entry.size) }),
        h('td', { class: 'mtime', text: entry.mtime_str }),
        h('td', { class: 'perms', text: entry.perms,
          title: canWrite() ? 'chmod' : '',
          onclick: canWrite() ? function () { doChmod(entry); } : null }),
        actions
      ]));
    });

    el('summary').textContent =
      res.dir_count + ' folders, ' + res.file_count + ' files · ' +
      humanSize(res.total_size);
    updateBatchBar();
  }

  function actBtn(sym, title, fn) {
    return h('button', { class: 'act', title: title, text: sym, onclick: fn });
  }

  function canWrite() { return api.ready && !api.readonly; }

  function updateBatchBar() {
    var n = Object.keys(state.selected).length;
    var bar = el('batchbar');
    if (n > 0 && canWrite()) { show(bar); el('batchcount').textContent = n; }
    else hide(bar);
  }

  // ---- operations --------------------------------------------------------
  async function doDownload(entry) {
    try {
      toast('Downloading ' + entry.name + '…', 'info');
      var blob = await api.download(entry.path);
      var url = URL.createObjectURL(blob);
      var a = h('a', { href: url, download: entry.name });
      document.body.appendChild(a); a.click(); a.remove();
      URL.revokeObjectURL(url);
    } catch (e) { toast(e.message || 'Download failed', 'error'); }
  }

  async function doRename(entry) {
    var name = prompt('Rename to:', entry.name);
    if (!name || name === entry.name) return;
    try { await api.call('rename', { path: entry.path, name: name });
      toast('Renamed', 'ok'); loadDir(state.path);
    } catch (e) { toast(e.message, 'error'); }
  }

  async function doDelete(entries) {
    var names = entries.map(function (e) { return e.name; });
    if (!confirm('Delete ' + names.length + ' item(s)?\n' + names.join('\n'))) return;
    try {
      await api.call('delete', { paths: entries.map(function (e) { return e.path; }) });
      toast('Deleted', 'ok'); loadDir(state.path);
    } catch (e) { toast(e.message, 'error'); }
  }

  async function doChmod(entry) {
    var mode = prompt('Permissions (octal), e.g. 644 or 755:',
      (entry.mode & 0o777).toString(8));
    if (!mode) return;
    try { await api.call('chmod', { path: entry.path, mode: mode });
      toast('Permissions updated', 'ok'); loadDir(state.path);
    } catch (e) { toast(e.message, 'error'); }
  }

  async function newFolder() {
    var name = prompt('New folder name:');
    if (!name) return;
    try { await api.call('mkdir', { path: state.path, name: name });
      toast('Folder created', 'ok'); loadDir(state.path);
    } catch (e) { toast(e.message, 'error'); }
  }

  async function newFile() {
    var name = prompt('New file name:');
    if (!name) return;
    try { await api.call('touch', { path: state.path, name: name });
      toast('File created', 'ok'); loadDir(state.path);
    } catch (e) { toast(e.message, 'error'); }
  }

  async function zipSelected() {
    var paths = Object.keys(state.selected);
    if (!paths.length) return;
    try { await api.call('zip', { paths: paths, dest: state.path });
      toast('Archive created', 'ok'); loadDir(state.path);
    } catch (e) { toast(e.message, 'error'); }
  }

  // ---- editor ------------------------------------------------------------
  async function openEditor(entry) {
    try {
      var res = await api.call('read', { path: entry.path });
      el('editor-name').textContent = entry.name;
      el('editor-text').value = res.content;
      el('editor-text').readOnly = !canWrite();
      el('editor-save').style.display = canWrite() ? '' : 'none';
      el('editor-save').onclick = async function () {
        try {
          await api.call('save', { path: entry.path, content: el('editor-text').value });
          toast('Saved', 'ok'); closeEditor();
        } catch (e) { toast(e.message, 'error'); }
      };
      show(el('editor-modal'));
    } catch (e) { toast(e.message, 'error'); }
  }
  function closeEditor() { hide(el('editor-modal')); }

  // ---- upload ------------------------------------------------------------
  async function uploadFiles(files) {
    for (var i = 0; i < files.length; i++) {
      var file = files[i];
      try {
        await api.upload(state.path, file, function (sent, total) {
          el('upstatus').textContent =
            file.name + ' ' + Math.round((sent / (total || 1)) * 100) + '%';
        });
        toast('Uploaded ' + file.name, 'ok');
      } catch (e) { toast('Upload failed: ' + (e.message || file.name), 'error'); }
    }
    el('upstatus').textContent = '';
    loadDir(state.path);
  }

  // ---- auth / bootstrap --------------------------------------------------
  async function doLogin(ev) {
    ev.preventDefault();
    var out = await api.login(el('login-user').value, el('login-pass').value);
    if (out.ok) { hide(el('login-screen')); enterApp(); }
    else el('login-error').textContent = out.error || 'Login failed';
  }

  async function enterApp() {
    show(el('app'));
    try { state.info = await api.call('info', {}); } catch (e) { /* noop */ }
    if (state.info) {
      el('title').textContent = state.info.title || 'FileManager';
      document.title = state.info.title || 'FileManager';
      el('whoami').textContent = state.info.username || '';
      if (state.info.readonly) show(el('ro-badge'));
      if (api.readonly) { hide(el('tb-write')); }
    }
    bindUi();
    loadDir('');
  }

  function bindUi() {
    el('btn-newfolder').onclick = newFolder;
    el('btn-newfile').onclick = newFile;
    el('btn-upload').onclick = function () { el('fileinput').click(); };
    el('fileinput').onchange = function () { uploadFiles(this.files); this.value = ''; };
    el('btn-logout').onclick = async function () { await api.logout(); location.reload(); };
    el('editor-close').onclick = closeEditor;
    el('batch-delete').onclick = function () {
      doDelete(Object.keys(state.selected).map(function (k) { return state.selected[k]; }));
    };
    el('batch-zip').onclick = zipSelected;
    el('search').oninput = debounce(onSearch, 300);

    var drop = el('droparea');
    ['dragover', 'dragenter'].forEach(function (ev) {
      document.addEventListener(ev, function (e) { e.preventDefault(); drop.classList.add('active'); });
    });
    ['dragleave', 'drop'].forEach(function (ev) {
      document.addEventListener(ev, function (e) { e.preventDefault();
        if (ev === 'drop' && e.dataTransfer.files.length) uploadFiles(e.dataTransfer.files);
        drop.classList.remove('active'); });
    });
  }

  async function onSearch() {
    var q = el('search').value.trim();
    if (!q) { loadDir(state.path); return; }
    try {
      var res = await api.call('search', { path: state.path, query: q });
      state.entries = res.results; state.selected = {};
      render({ entries: res.results, dir_count: 0, file_count: res.results.length, total_size: 0 });
    } catch (e) { toast(e.message, 'error'); }
  }

  function debounce(fn, ms) {
    var t; return function () { clearTimeout(t); t = setTimeout(fn, ms); };
  }

  async function boot() {
    if (!window.isSecureContext && location.hostname !== 'localhost'
        && location.hostname !== '127.0.0.1') {
      el('login-error').textContent =
        'WebCrypto requires HTTPS or localhost.';
    }
    try {
      var info = await api.connect();
      if (!info.authRequired) { hide(el('login-screen')); enterApp(); }
      else { show(el('login-screen')); el('login-user').focus(); }
    } catch (e) {
      el('login-error').textContent = 'Could not establish secure session.';
    }
    el('login-form').addEventListener('submit', doLogin);
  }

  document.addEventListener('DOMContentLoaded', boot);
})();
