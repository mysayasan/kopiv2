/* The setup wizard's behaviour. Plain ES5-compatible DOM code with no build step and no
   dependency: it runs before the app's bundle exists and on installs with no internet.

   The server is the authority on every rule here — this file only sequences the steps and
   renders what the server says. Client-side checks exist to keep the operator moving, not
   to decide anything. */
(function () {
  'use strict';

  // The four suite languages. Kept inline rather than reaching for the shared i18n
  // bundle, which is not built or served at this point in startup.
  var DICT = {
    en: {
      'head.sub': 'Finish configuring this install. The app starts as soon as you are done.',
      'head.language': 'Language',
      'head.theme': 'Theme',
      'theme.system': 'System', 'theme.light': 'Light', 'theme.dark': 'Dark',
      'step.welcome': 'Start', 'step.db': 'Database', 'step.cache': 'Cache',
      'step.web': 'Address', 'step.admin': 'Administrator', 'step.review': 'Review', 'step.done': 'Finish',
      'welcome.title': 'Before you start',
      'welcome.body': 'This page writes the settings the app reads at startup: its database, its cache, the ports it listens on, and the first administrator. Nothing is saved until the last step.',
      'welcome.file': 'Settings are written to',
      'db.title': 'Database', 'db.engine': 'Engine', 'db.host': 'Host', 'db.port': 'Port',
      'db.user': 'User', 'db.password': 'Password', 'db.name': 'Database name',
      'db.ssl': 'SSL mode', 'db.file': 'Database file',
      'db.sqliteOption': 'SQLite (single file, no server)',
      'cache.title': 'Cache', 'cache.provider': 'Provider', 'cache.inprocess': 'In-process (single server)',
      'cache.hint': 'Redis is required only when you run more than one instance behind a load balancer.',
      'cache.address': 'Address', 'cache.db': 'DB', 'cache.password': 'Password', 'cache.tls': 'Use TLS',
      'web.title': 'Web address', 'web.enableTls': 'Serve over HTTPS', 'web.tlsPorts': 'HTTPS ports',
      'web.nonTlsPorts': 'HTTP ports', 'web.hostnames': 'Hostnames',
      'web.hint': 'Separate several ports with commas. Leave a box empty to serve nothing on it.',
      'admin.title': 'Administrator', 'admin.enabled': 'Enable the built-in administrator account',
      'admin.username': 'Username', 'admin.password': 'Password',
      'admin.hint': 'Leave the password empty to have one generated. Either way you must change it on first sign-in.',
      'admin.hintSet': 'Leave the password empty to keep the one already configured. You must change it on first sign-in.',
      'review.title': 'Review', 'review.hint': 'Choosing Finish writes the settings and starts the app.',
      'done.title': 'Starting', 'done.body': 'The settings are saved and the app is starting. This page can be closed.',
      'done.note': 'Waiting for the app to answer…',
      'done.slow': 'The app is still starting. Open the address above once it is ready.',
      'action.test': 'Test connection', 'action.testing': 'Testing…', 'action.back': 'Back',
      'action.next': 'Next', 'action.finish': 'Finish', 'action.saving': 'Saving…',
      'result.ok': 'Connected', 'result.noConnectionNeeded': 'No connection needed', 'result.notVerified': 'Not verified',
      'error.load': 'Could not read the current settings.',
      'error.generic': 'Something went wrong.',
      'value.none': 'none', 'value.generated': 'generated on first start', 'value.kept': 'unchanged', 'value.disabled': 'disabled'
    },
    ms: {
      'head.sub': 'Lengkapkan tetapan pemasangan ini. Aplikasi akan bermula sebaik sahaja anda selesai.',
      'head.language': 'Bahasa',
      'head.theme': 'Tema',
      'theme.system': 'Sistem', 'theme.light': 'Cerah', 'theme.dark': 'Gelap',
      'step.welcome': 'Mula', 'step.db': 'Pangkalan data', 'step.cache': 'Cache',
      'step.web': 'Alamat', 'step.admin': 'Pentadbir', 'step.review': 'Semak', 'step.done': 'Selesai',
      'welcome.title': 'Sebelum anda mula',
      'welcome.body': 'Halaman ini menulis tetapan yang dibaca aplikasi semasa permulaan: pangkalan data, cache, port yang didengarinya, dan pentadbir pertama. Tiada apa-apa disimpan sehingga langkah terakhir.',
      'welcome.file': 'Tetapan ditulis ke',
      'db.title': 'Pangkalan data', 'db.engine': 'Enjin', 'db.host': 'Hos', 'db.port': 'Port',
      'db.user': 'Pengguna', 'db.password': 'Kata laluan', 'db.name': 'Nama pangkalan data',
      'db.ssl': 'Mod SSL', 'db.file': 'Fail pangkalan data',
      'db.sqliteOption': 'SQLite (fail tunggal, tiada pelayan)',
      'cache.title': 'Cache', 'cache.provider': 'Penyedia', 'cache.inprocess': 'Dalam proses (satu pelayan)',
      'cache.hint': 'Redis diperlukan hanya apabila anda menjalankan lebih daripada satu instans di belakang pengimbang beban.',
      'cache.address': 'Alamat', 'cache.db': 'DB', 'cache.password': 'Kata laluan', 'cache.tls': 'Guna TLS',
      'web.title': 'Alamat web', 'web.enableTls': 'Sajikan melalui HTTPS', 'web.tlsPorts': 'Port HTTPS',
      'web.nonTlsPorts': 'Port HTTP', 'web.hostnames': 'Nama hos',
      'web.hint': 'Pisahkan beberapa port dengan koma. Biarkan kotak kosong untuk tidak menyajikan apa-apa padanya.',
      'admin.title': 'Pentadbir', 'admin.enabled': 'Aktifkan akaun pentadbir terbina dalam',
      'admin.username': 'Nama pengguna', 'admin.password': 'Kata laluan',
      'admin.hint': 'Biarkan kata laluan kosong untuk menjanakannya. Anda tetap perlu menukarnya semasa log masuk pertama.',
      'admin.hintSet': 'Biarkan kata laluan kosong untuk mengekalkan yang sedia ada. Anda perlu menukarnya semasa log masuk pertama.',
      'review.title': 'Semak', 'review.hint': 'Memilih Selesai akan menulis tetapan dan memulakan aplikasi.',
      'done.title': 'Sedang bermula', 'done.body': 'Tetapan telah disimpan dan aplikasi sedang bermula. Halaman ini boleh ditutup.',
      'done.note': 'Menunggu aplikasi menjawab…',
      'done.slow': 'Aplikasi masih bermula. Buka alamat di atas apabila ia sedia.',
      'action.test': 'Uji sambungan', 'action.testing': 'Menguji…', 'action.back': 'Kembali',
      'action.next': 'Seterusnya', 'action.finish': 'Selesai', 'action.saving': 'Menyimpan…',
      'result.ok': 'Bersambung', 'result.noConnectionNeeded': 'Tiada sambungan diperlukan', 'result.notVerified': 'Tidak disahkan',
      'error.load': 'Tetapan semasa tidak dapat dibaca.',
      'error.generic': 'Berlaku ralat.',
      'value.none': 'tiada', 'value.generated': 'dijana semasa mula pertama', 'value.kept': 'tidak berubah', 'value.disabled': 'dilumpuhkan'
    },
    zh: {
      'head.sub': '完成本次安装的配置。完成后应用会立即启动。',
      'head.language': '语言',
      'head.theme': '主题',
      'theme.system': '跟随系统', 'theme.light': '浅色', 'theme.dark': '深色',
      'step.welcome': '开始', 'step.db': '数据库', 'step.cache': '缓存',
      'step.web': '地址', 'step.admin': '管理员', 'step.review': '核对', 'step.done': '完成',
      'welcome.title': '开始之前',
      'welcome.body': '此页面写入应用启动时读取的设置：数据库、缓存、监听端口以及第一个管理员。在最后一步之前不会保存任何内容。',
      'welcome.file': '设置将写入',
      'db.title': '数据库', 'db.engine': '引擎', 'db.host': '主机', 'db.port': '端口',
      'db.user': '用户', 'db.password': '密码', 'db.name': '数据库名称',
      'db.ssl': 'SSL 模式', 'db.file': '数据库文件',
      'db.sqliteOption': 'SQLite（单文件，无需服务器）',
      'cache.title': '缓存', 'cache.provider': '提供方', 'cache.inprocess': '进程内（单服务器）',
      'cache.hint': '只有在负载均衡器后运行多个实例时才需要 Redis。',
      'cache.address': '地址', 'cache.db': '库', 'cache.password': '密码', 'cache.tls': '使用 TLS',
      'web.title': 'Web 地址', 'web.enableTls': '通过 HTTPS 提供服务', 'web.tlsPorts': 'HTTPS 端口',
      'web.nonTlsPorts': 'HTTP 端口', 'web.hostnames': '主机名',
      'web.hint': '多个端口用逗号分隔。留空表示不在其上提供服务。',
      'admin.title': '管理员', 'admin.enabled': '启用内置管理员账户',
      'admin.username': '用户名', 'admin.password': '密码',
      'admin.hint': '留空密码将自动生成一个。无论如何，首次登录时都必须修改。',
      'admin.hintSet': '留空密码将保留已配置的密码。首次登录时必须修改。',
      'review.title': '核对', 'review.hint': '选择“完成”将写入设置并启动应用。',
      'done.title': '正在启动', 'done.body': '设置已保存，应用正在启动。可以关闭此页面。',
      'done.note': '正在等待应用响应…',
      'done.slow': '应用仍在启动。就绪后请打开上面的地址。',
      'action.test': '测试连接', 'action.testing': '测试中…', 'action.back': '返回',
      'action.next': '下一步', 'action.finish': '完成', 'action.saving': '保存中…',
      'result.ok': '已连接', 'result.noConnectionNeeded': '无需连接', 'result.notVerified': '未验证',
      'error.load': '无法读取当前设置。',
      'error.generic': '发生错误。',
      'value.none': '无', 'value.generated': '首次启动时生成', 'value.kept': '保持不变', 'value.disabled': '已禁用'
    },
    ar: {
      'head.sub': 'أكمل إعداد هذا التثبيت. سيبدأ التطبيق فور انتهائك.',
      'head.language': 'اللغة',
      'head.theme': 'السمة',
      'theme.system': 'حسب النظام', 'theme.light': 'فاتح', 'theme.dark': 'داكن',
      'step.welcome': 'البداية', 'step.db': 'قاعدة البيانات', 'step.cache': 'الذاكرة المؤقتة',
      'step.web': 'العنوان', 'step.admin': 'المسؤول', 'step.review': 'المراجعة', 'step.done': 'إنهاء',
      'welcome.title': 'قبل أن تبدأ',
      'welcome.body': 'تكتب هذه الصفحة الإعدادات التي يقرأها التطبيق عند بدء التشغيل: قاعدة البيانات، والذاكرة المؤقتة، والمنافذ التي يستمع إليها، والمسؤول الأول. لا يُحفظ شيء قبل الخطوة الأخيرة.',
      'welcome.file': 'تُكتب الإعدادات إلى',
      'db.title': 'قاعدة البيانات', 'db.engine': 'المحرك', 'db.host': 'المضيف', 'db.port': 'المنفذ',
      'db.user': 'المستخدم', 'db.password': 'كلمة المرور', 'db.name': 'اسم قاعدة البيانات',
      'db.ssl': 'وضع SSL', 'db.file': 'ملف قاعدة البيانات',
      'db.sqliteOption': 'SQLite (ملف واحد، بدون خادم)',
      'cache.title': 'الذاكرة المؤقتة', 'cache.provider': 'المزوّد', 'cache.inprocess': 'داخل العملية (خادم واحد)',
      'cache.hint': 'لا حاجة إلى Redis إلا عند تشغيل أكثر من نسخة خلف موزّع أحمال.',
      'cache.address': 'العنوان', 'cache.db': 'القاعدة', 'cache.password': 'كلمة المرور', 'cache.tls': 'استخدام TLS',
      'web.title': 'عنوان الويب', 'web.enableTls': 'التقديم عبر HTTPS', 'web.tlsPorts': 'منافذ HTTPS',
      'web.nonTlsPorts': 'منافذ HTTP', 'web.hostnames': 'أسماء المضيفين',
      'web.hint': 'افصل بين المنافذ بفواصل. اترك الحقل فارغًا لعدم التقديم عليه.',
      'admin.title': 'المسؤول', 'admin.enabled': 'تفعيل حساب المسؤول المدمج',
      'admin.username': 'اسم المستخدم', 'admin.password': 'كلمة المرور',
      'admin.hint': 'اترك كلمة المرور فارغة ليتم توليد واحدة. في الحالتين يجب تغييرها عند أول تسجيل دخول.',
      'admin.hintSet': 'اترك كلمة المرور فارغة للإبقاء على المهيّأة حاليًا. يجب تغييرها عند أول تسجيل دخول.',
      'review.title': 'المراجعة', 'review.hint': 'اختيار «إنهاء» يكتب الإعدادات ويبدأ التطبيق.',
      'done.title': 'جارٍ البدء', 'done.body': 'حُفظت الإعدادات والتطبيق يبدأ الآن. يمكن إغلاق هذه الصفحة.',
      'done.note': 'في انتظار استجابة التطبيق…',
      'done.slow': 'ما زال التطبيق يبدأ. افتح العنوان أعلاه عندما يصبح جاهزًا.',
      'action.test': 'اختبار الاتصال', 'action.testing': 'جارٍ الاختبار…', 'action.back': 'رجوع',
      'action.next': 'التالي', 'action.finish': 'إنهاء', 'action.saving': 'جارٍ الحفظ…',
      'result.ok': 'تم الاتصال', 'result.noConnectionNeeded': 'لا حاجة إلى اتصال', 'result.notVerified': 'غير مُتحقَّق منه',
      'error.load': 'تعذّرت قراءة الإعدادات الحالية.',
      'error.generic': 'حدث خطأ.',
      'value.none': 'لا شيء', 'value.generated': 'يُولَّد عند أول تشغيل', 'value.kept': 'دون تغيير', 'value.disabled': 'معطّل'
    }
  };

  var STEPS = ['step.welcome', 'step.db', 'step.cache', 'step.web', 'step.admin', 'step.review', 'step.done'];
  var LAST_INPUT_STEP = 5; // the review pane; step 6 is the post-write "starting" pane

  var lang = 'en';
  var step = 0;
  var state = null;
  var token = new URLSearchParams(window.location.search).get('t') || '';

  /* ---------- theme ---------- */

  // Same contract as the app: a theme-<name> class on <html>. "System" is resolved here
  // rather than in CSS so the stylesheet carries exactly two palettes, and so the page
  // keeps following the OS live while System stays selected.
  //
  // The key is scoped to the setup page on purpose. This wizard is served from its own
  // port, so it is a different browser ORIGIN from the app and could not read the app's
  // stored preference even if it used the same name.
  var THEME_KEY = 'kopiv2_setup_theme';
  var THEMES = ['system', 'light', 'dark'];
  var darkQuery = window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)') : null;
  var themePref = 'system';

  function readStoredTheme() {
    try {
      var stored = localStorage.getItem(THEME_KEY);
      return THEMES.indexOf(stored) >= 0 ? stored : 'system';
    } catch (_) {
      // Private windows and blocked site data throw on read. Following the OS is the
      // right answer when we cannot remember a choice.
      return 'system';
    }
  }

  function resolveTheme(pref) {
    if (pref === 'light' || pref === 'dark') return pref;
    return darkQuery && darkQuery.matches ? 'dark' : 'light';
  }

  function applyTheme(pref) {
    themePref = THEMES.indexOf(pref) >= 0 ? pref : 'system';
    var root = document.documentElement;
    root.classList.remove('theme-light', 'theme-dark');
    root.classList.add('theme-' + resolveTheme(themePref));
  }

  function changeTheme(pref) {
    applyTheme(pref);
    try { localStorage.setItem(THEME_KEY, themePref); } catch (_) {}
  }

  // Applied immediately — this script is loaded in the head, so the class lands on
  // <html> before the body paints and a dark-theme operator never sees a white flash.
  applyTheme(readStoredTheme());

  if (darkQuery) {
    var onSchemeChange = function () { if (themePref === 'system') applyTheme('system'); };
    // addEventListener is the modern form; addListener is kept for older WebViews, which
    // an appliance's bundled browser can still be.
    if (darkQuery.addEventListener) darkQuery.addEventListener('change', onSchemeChange);
    else if (darkQuery.addListener) darkQuery.addListener(onSchemeChange);
  }

  function $(id) { return document.getElementById(id); }
  function t(key) { return (DICT[lang] && DICT[lang][key]) || DICT.en[key] || key; }

  function api(path, body) {
    var opts = { method: body === undefined ? 'GET' : 'POST', headers: {} };
    if (token) { opts.headers['X-Setup-Token'] = token; }
    if (body !== undefined) {
      opts.headers['Content-Type'] = 'application/json';
      opts.body = JSON.stringify(body);
    }
    return fetch(path, opts).then(function (r) {
      return r.json().catch(function () { return {}; }).then(function (data) {
        // A non-2xx with a JSON body still carries the operator-facing message, so it
        // is passed through rather than replaced with a status code.
        if (!r.ok && !data.error) { data.error = 'HTTP ' + r.status; }
        return data;
      });
    });
  }

  /* ---------- rendering ---------- */

  function applyLanguage() {
    document.documentElement.lang = lang;
    document.documentElement.dir = lang === 'ar' ? 'rtl' : 'ltr';
    var nodes = document.querySelectorAll('[data-i18n]');
    for (var i = 0; i < nodes.length; i++) {
      nodes[i].textContent = t(nodes[i].getAttribute('data-i18n'));
    }
    $('admin-hint').textContent = state && state.adminPasswordSet ? t('admin.hintSet') : t('admin.hint');
    renderSteps();
    renderNav();
    if (step === LAST_INPUT_STEP) { renderReview(); }
  }

  function renderSteps() {
    var ol = $('steps');
    ol.textContent = '';
    for (var i = 0; i < STEPS.length; i++) {
      var li = document.createElement('li');
      li.textContent = t(STEPS[i]);
      li.className = i === step ? 'active' : (i < step ? 'done' : '');
      ol.appendChild(li);
    }
  }

  function renderNav() {
    var done = step > LAST_INPUT_STEP;
    $('back').hidden = step === 0 || done;
    $('next').hidden = done;
    $('next').textContent = step === LAST_INPUT_STEP ? t('action.finish') : t('action.next');
  }

  function showStep(n) {
    step = n;
    for (var i = 0; i < STEPS.length; i++) {
      var pane = $('pane-' + i);
      if (pane) { pane.hidden = i !== n; }
    }
    hideAlert();
    if (n === LAST_INPUT_STEP) { renderReview(); }
    renderSteps();
    renderNav();
  }

  function showAlert(message) {
    var box = $('alert');
    box.textContent = message;
    box.hidden = false;
    box.scrollIntoView({ block: 'nearest' });
  }

  function hideAlert() { $('alert').hidden = true; }

  /* ---------- form <-> answers ---------- */

  function portsToText(list) { return (list || []).join(', '); }

  function textToPorts(text) {
    var out = [];
    var parts = String(text || '').split(',');
    for (var i = 0; i < parts.length; i++) {
      var trimmed = parts[i].trim();
      if (!trimmed) { continue; }
      var n = parseInt(trimmed, 10);
      // A non-numeric entry becomes 0, which the server rejects by name rather than
      // being dropped here as if the operator had never typed it.
      out.push(isNaN(n) ? 0 : n);
    }
    return out;
  }

  function fillForm(answers) {
    $('db-engine').value = answers.db.engine || 'postgres';
    $('db-host').value = answers.db.host || '';
    $('db-port').value = answers.db.port || '';
    $('db-user').value = answers.db.user || '';
    $('db-name').value = answers.db.engine === 'sqlite' ? '' : (answers.db.db_name || '');
    $('db-fileName').value = answers.db.engine === 'sqlite' ? (answers.db.db_name || '') : '';
    $('db-ssl').value = answers.db.ssl_mode || '';

    $('cache-provider').value = answers.cache.provider === 'redis' ? 'redis' : 'default';
    $('cache-address').value = answers.cache.address || '';
    $('cache-db').value = answers.cache.db || 0;
    $('cache-tls').checked = !!answers.cache.useTls;

    $('web-tls').checked = !!answers.web.enableTls;
    $('web-tlsPorts').value = portsToText(answers.web.tlsPorts);
    $('web-nonTlsPorts').value = portsToText(answers.web.nonTlsPorts);
    $('web-hostnames').value = (answers.web.hostnames || ['*']).join(', ');

    $('admin-enabled').checked = !!answers.admin.enabled;
    $('admin-username').value = answers.admin.username || '';

    syncEngine();
    syncCacheProvider();
  }

  function readDB() {
    var engine = $('db-engine').value;
    var sqlite = engine === 'sqlite';
    return {
      engine: engine,
      host: sqlite ? '' : $('db-host').value,
      port: sqlite ? 0 : parseInt($('db-port').value, 10) || 0,
      user: sqlite ? '' : $('db-user').value,
      password: sqlite ? '' : $('db-password').value,
      db_name: sqlite ? $('db-fileName').value : $('db-name').value,
      ssl_mode: sqlite ? '' : $('db-ssl').value
    };
  }

  function readCache() {
    return {
      provider: $('cache-provider').value,
      address: $('cache-address').value,
      password: $('cache-password').value,
      db: parseInt($('cache-db').value, 10) || 0,
      useTls: $('cache-tls').checked
    };
  }

  function readAnswers() {
    return {
      db: readDB(),
      cache: readCache(),
      web: {
        enableTls: $('web-tls').checked,
        tlsPorts: textToPorts($('web-tlsPorts').value),
        nonTlsPorts: textToPorts($('web-nonTlsPorts').value),
        hostnames: String($('web-hostnames').value || '*').split(',').map(function (s) { return s.trim(); }).filter(Boolean)
      },
      admin: {
        enabled: $('admin-enabled').checked,
        username: $('admin-username').value,
        password: $('admin-password').value
      }
    };
  }

  function syncEngine() {
    var sqlite = $('db-engine').value === 'sqlite';
    $('db-server').hidden = sqlite;
    $('db-file').hidden = !sqlite;
    setResult('db-result', null);
  }

  function syncCacheProvider() {
    $('cache-redis').hidden = $('cache-provider').value !== 'redis';
    setResult('cache-result', null);
  }

  function setResult(id, ok, message) {
    var node = $(id);
    if (ok === null) { node.textContent = ''; node.className = 'result'; return; }
    node.textContent = message || (ok ? t('result.ok') : '');
    node.className = 'result ' + (ok ? 'ok' : 'bad');
  }

  /* ---------- review ---------- */

  function reviewRow(dl, label, value) {
    var row = document.createElement('div');
    var dt = document.createElement('dt');
    var dd = document.createElement('dd');
    dt.textContent = label;
    // Values are host names, ports and paths — verbatim, left-to-right even in Arabic,
    // which is what bdi keeps from reordering around the surrounding RTL text.
    var bdi = document.createElement('bdi');
    bdi.dir = 'ltr';
    bdi.textContent = value;
    dd.appendChild(bdi);
    row.appendChild(dt);
    row.appendChild(dd);
    dl.appendChild(row);
  }

  function renderReview() {
    var a = readAnswers();
    var dl = $('review');
    dl.textContent = '';
    if (a.db.engine === 'sqlite') {
      reviewRow(dl, t('db.title'), 'SQLite — ' + (a.db.db_name || t('value.none')));
    } else {
      reviewRow(dl, t('db.title'), a.db.engine + ' — ' + a.db.user + '@' + a.db.host + ':' + a.db.port + '/' + a.db.db_name);
    }
    reviewRow(dl, t('cache.title'), a.cache.provider === 'redis' ? 'Redis — ' + a.cache.address : t('cache.inprocess'));
    reviewRow(dl, t('web.tlsPorts'), a.web.enableTls && a.web.tlsPorts.length ? portsToText(a.web.tlsPorts) : t('value.none'));
    reviewRow(dl, t('web.nonTlsPorts'), a.web.nonTlsPorts.length ? portsToText(a.web.nonTlsPorts) : t('value.none'));
    reviewRow(dl, t('web.hostnames'), a.web.hostnames.join(', '));
    if (!a.admin.enabled) {
      reviewRow(dl, t('admin.title'), t('value.disabled'));
    } else {
      var pw = a.admin.password ? '••••••••' : (state && state.adminPasswordSet ? t('value.kept') : t('value.generated'));
      reviewRow(dl, t('admin.title'), a.admin.username + ' — ' + pw);
    }
  }

  /* ---------- actions ---------- */

  function testConnection(kind) {
    var button = $(kind + '-test');
    var original = button.textContent;
    button.disabled = true;
    button.textContent = t('action.testing');
    setResult(kind + '-result', null);
    api('/api/test/' + kind, kind === 'db' ? readDB() : readCache()).then(function (res) {
      // res.note is a CODE from the server (see handleTestCache); the wording lives here
      // so it can be localized. An unrecognized code falls back to the plain success text
      // rather than showing the operator a raw identifier.
      var note = res.note ? t('result.' + res.note) : '';
      if (note === 'result.' + res.note) note = '';
      setResult(kind + '-result', !!res.ok, res.ok ? (note || t('result.ok')) : res.error);
    }).catch(function (err) {
      setResult(kind + '-result', false, String(err));
    }).then(function () {
      button.disabled = false;
      button.textContent = original;
    });
  }

  function finish() {
    var button = $('next');
    button.disabled = true;
    button.textContent = t('action.saving');
    hideAlert();
    api('/api/complete', readAnswers()).then(function (res) {
      if (!res.ok) {
        showAlert(res.error || t('error.generic'));
        button.disabled = false;
        renderNav();
        return;
      }
      showStep(6);
      var link = $('startUrl');
      link.textContent = res.startUrl;
      link.href = res.startUrl;
      waitForApp(res.startUrl);
    }).catch(function (err) {
      showAlert(String(err));
      button.disabled = false;
      renderNav();
    });
  }

  // waitForApp polls the app's own URL and follows it once it answers. The probe is
  // no-cors because the app is a different origin, so all it can learn is "something
  // answered" — which is exactly the question. A self-signed HTTPS certificate never
  // resolves here, so after a bounded wait the page stops guessing and leaves the
  // operator the link rather than spinning forever.
  function waitForApp(url) {
    if (!url) { $('done-note').textContent = t('done.slow'); return; }
    var attempts = 0;
    var timer = setInterval(function () {
      attempts++;
      if (attempts > 20) {
        clearInterval(timer);
        $('done-note').textContent = t('done.slow');
        return;
      }
      fetch(url, { mode: 'no-cors', cache: 'no-store' }).then(function () {
        clearInterval(timer);
        window.location.href = url;
      }).catch(function () { /* not up yet */ });
    }, 2000);
  }

  /* ---------- wiring ---------- */

  function start() {
    $('lang').addEventListener('change', function () { lang = this.value; applyLanguage(); });
    $('theme').value = themePref;
    $('theme').addEventListener('change', function () { changeTheme(this.value); });
    $('db-engine').addEventListener('change', syncEngine);
    $('cache-provider').addEventListener('change', syncCacheProvider);
    $('db-test').addEventListener('click', function () { testConnection('db'); });
    $('cache-test').addEventListener('click', function () { testConnection('cache'); });
    $('back').addEventListener('click', function () { if (step > 0) { showStep(step - 1); } });
    $('next').addEventListener('click', function () {
      if (step === LAST_INPUT_STEP) { finish(); return; }
      showStep(step + 1);
    });

    api('/api/state').then(function (data) {
      if (!data || !data.answers) { showAlert(t('error.load')); return; }
      state = data;
      document.title = data.app + ' — ' + t('step.welcome');
      $('appTitle').textContent = data.app;
      $('configPath').textContent = data.configPath;
      $('db-test').disabled = !data.canTestDb;
      $('cache-test').disabled = !data.canTestCache;
      fillForm(data.answers);
      applyLanguage();
      showStep(0);
    }).catch(function () {
      showAlert(t('error.load'));
    });

    applyLanguage();
    showStep(0);
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', start);
  } else {
    start();
  }
})();
