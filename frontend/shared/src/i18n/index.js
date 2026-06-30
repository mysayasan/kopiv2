// Lightweight, dependency-free i18n for the shared UI module — mirrors the apps'
// existing theme pattern (a value persisted in localStorage + a dropdown), so no
// library and no build step are needed. English is the base; the other locales are
// AI-translated UI-control vocabulary (short, standardized terms).
//
// Usage in a shared component:  const t = useT();  t('common.apply')
//   - keys missing from the active locale fall back to English, then to the key
//     itself, so adding a key can never crash a render;
//   - `t(key, vars)` interpolates `{name}` placeholders, e.g.
//     t('pager.summary', { current, count, total }).
//
// Apps opt in by wrapping their tree in <LangProvider lang={lang}> and feeding it a
// locale from localStorage. With NO provider, useT() resolves to English, so every
// existing consumer keeps working unchanged until an app wires the provider.
import { createContext, useContext, useMemo } from 'react';

// Each dictionary is a flat map of stable id -> string. Namespaced by area
// (common.* reused across widgets, table.*, pager.*, nav.*, lang.*).
const en = {
  'common.close': 'Close',
  'common.remove': 'Remove',
  'common.add': 'Add',
  'common.clear': 'Clear',
  'common.apply': 'Apply',
  'common.any': 'Any',
  'common.yes': 'Yes',
  'common.no': 'No',
  'common.dismiss': 'Dismiss',
  // Curated high-frequency generic terms reused across apps (translated once here).
  'common.save': 'Save',
  'common.saving': 'Saving…',
  'common.saved': 'Saved',
  'common.cancel': 'Cancel',
  'common.delete': 'Delete',
  'common.edit': 'Edit',
  'common.create': 'Create',
  'common.update': 'Update',
  'common.refresh': 'Refresh',
  'common.reload': 'Reload',
  'common.enable': 'Enable',
  'common.disable': 'Disable',
  'common.enabled': 'Enabled',
  'common.disabled': 'Disabled',
  'common.active': 'Active',
  'common.inactive': 'Inactive',
  'common.loading': 'Loading…',
  'common.name': 'Name',
  'common.description': 'Description',
  'common.username': 'Username',
  'common.password': 'Password',
  'common.search': 'Search',
  'common.test': 'Test',
  'common.upload': 'Upload',
  'common.download': 'Download',
  'common.start': 'Start',
  'common.stop': 'Stop',
  'common.confirm': 'Confirm',
  'common.reset': 'Reset',
  'common.discard': 'Discard',
  'common.back': 'Back',
  'common.next': 'Next',
  'common.done': 'Done',
  'common.status': 'Status',
  'common.type': 'Type',
  'common.all': 'All',
  'common.none': 'None',
  'common.optional': '(optional)',
  'table.empty': 'No records',
  'table.sort': 'Sort',
  'table.sortAsc': 'ASC',
  'table.sortDesc': 'DESC',
  'table.operator': 'Operator',
  'table.value': 'Value',
  'table.condition': 'Condition {n}',
  'table.filterCol': 'Filter {col}',
  'table.sortByCol': 'Sort by {col}',
  'pager.summary': 'Page {current} / {count} · {total} total',
  'pager.first': 'First page',
  'pager.previous': 'Previous page',
  'pager.next': 'Next page',
  'pager.last': 'Last page',
  'pager.goto': 'Go to page',
  'pager.pageNumber': 'Page number',
  'nav.main': 'Main',
  'lang.label': 'Language',
  'lang.select': 'Select language',
  'foot.built': 'built {date}',
  'foot.core': 'core v{v}',
  'foot.commit': 'commit',
  'foot.tagline': 'An r450k product — showing how human engineering and AI complete each other.',
};

// Malay (Bahasa Melayu) — Malaysia's national language.
const ms = {
  'common.close': 'Tutup',
  'common.remove': 'Buang',
  'common.add': 'Tambah',
  'common.clear': 'Kosongkan',
  'common.apply': 'Guna',
  'common.any': 'Mana-mana',
  'common.yes': 'Ya',
  'common.no': 'Tidak',
  'common.dismiss': 'Tutup',
  'common.save': 'Simpan',
  'common.saving': 'Menyimpan…',
  'common.saved': 'Disimpan',
  'common.cancel': 'Batal',
  'common.delete': 'Padam',
  'common.edit': 'Edit',
  'common.create': 'Cipta',
  'common.update': 'Kemas kini',
  'common.refresh': 'Muat semula',
  'common.reload': 'Muat semula',
  'common.enable': 'Dayakan',
  'common.disable': 'Lumpuhkan',
  'common.enabled': 'Didayakan',
  'common.disabled': 'Dilumpuhkan',
  'common.active': 'Aktif',
  'common.inactive': 'Tidak aktif',
  'common.loading': 'Memuatkan…',
  'common.name': 'Nama',
  'common.description': 'Penerangan',
  'common.username': 'Nama pengguna',
  'common.password': 'Kata laluan',
  'common.search': 'Cari',
  'common.test': 'Uji',
  'common.upload': 'Muat naik',
  'common.download': 'Muat turun',
  'common.start': 'Mula',
  'common.stop': 'Henti',
  'common.confirm': 'Sahkan',
  'common.reset': 'Set semula',
  'common.discard': 'Buang',
  'common.back': 'Kembali',
  'common.next': 'Seterusnya',
  'common.done': 'Selesai',
  'common.status': 'Status',
  'common.type': 'Jenis',
  'common.all': 'Semua',
  'common.none': 'Tiada',
  'common.optional': '(pilihan)',
  'table.empty': 'Tiada rekod',
  'table.sort': 'Isih',
  'table.sortAsc': 'Menaik',
  'table.sortDesc': 'Menurun',
  'table.operator': 'Operator',
  'table.value': 'Nilai',
  'table.condition': 'Syarat {n}',
  'table.filterCol': 'Tapis {col}',
  'table.sortByCol': 'Isih mengikut {col}',
  'pager.summary': 'Halaman {current} / {count} · {total} jumlah',
  'pager.first': 'Halaman pertama',
  'pager.previous': 'Halaman sebelumnya',
  'pager.next': 'Halaman seterusnya',
  'pager.last': 'Halaman terakhir',
  'pager.goto': 'Pergi ke halaman',
  'pager.pageNumber': 'Nombor halaman',
  'nav.main': 'Utama',
  'lang.label': 'Bahasa',
  'lang.select': 'Pilih bahasa',
  'foot.built': 'dibina {date}',
  'foot.core': 'teras v{v}',
  'foot.commit': 'commit',
  'foot.tagline': 'Produk r450k — menunjukkan bagaimana kejuruteraan manusia dan AI saling melengkapi.',
};

// Chinese (Simplified).
const zh = {
  'common.close': '关闭',
  'common.remove': '移除',
  'common.add': '添加',
  'common.clear': '清除',
  'common.apply': '应用',
  'common.any': '任意',
  'common.yes': '是',
  'common.no': '否',
  'common.dismiss': '关闭',
  'common.save': '保存',
  'common.saving': '正在保存…',
  'common.saved': '已保存',
  'common.cancel': '取消',
  'common.delete': '删除',
  'common.edit': '编辑',
  'common.create': '创建',
  'common.update': '更新',
  'common.refresh': '刷新',
  'common.reload': '重新加载',
  'common.enable': '启用',
  'common.disable': '停用',
  'common.enabled': '已启用',
  'common.disabled': '已停用',
  'common.active': '活动',
  'common.inactive': '未激活',
  'common.loading': '加载中…',
  'common.name': '名称',
  'common.description': '说明',
  'common.username': '用户名',
  'common.password': '密码',
  'common.search': '搜索',
  'common.test': '测试',
  'common.upload': '上传',
  'common.download': '下载',
  'common.start': '开始',
  'common.stop': '停止',
  'common.confirm': '确认',
  'common.reset': '重置',
  'common.discard': '放弃',
  'common.back': '上一步',
  'common.next': '下一步',
  'common.done': '完成',
  'common.status': '状态',
  'common.type': '类型',
  'common.all': '全部',
  'common.none': '无',
  'common.optional': '（可选）',
  'table.empty': '无记录',
  'table.sort': '排序',
  'table.sortAsc': '升序',
  'table.sortDesc': '降序',
  'table.operator': '运算符',
  'table.value': '值',
  'table.condition': '条件 {n}',
  'table.filterCol': '筛选 {col}',
  'table.sortByCol': '按 {col} 排序',
  'pager.summary': '第 {current} / {count} 页 · 共 {total} 条',
  'pager.first': '首页',
  'pager.previous': '上一页',
  'pager.next': '下一页',
  'pager.last': '末页',
  'pager.goto': '跳转到页面',
  'pager.pageNumber': '页码',
  'nav.main': '主导航',
  'lang.label': '语言',
  'lang.select': '选择语言',
  'foot.built': '构建于 {date}',
  'foot.core': '核心 v{v}',
  'foot.commit': '提交',
  'foot.tagline': 'r450k 产品 — 展示人类工程与 AI 如何相辅相成。',
};

// Tamil.
const ta = {
  'common.close': 'மூடு',
  'common.remove': 'அகற்று',
  'common.add': 'சேர்',
  'common.clear': 'அழி',
  'common.apply': 'பயன்படுத்து',
  'common.any': 'ஏதேனும்',
  'common.yes': 'ஆம்',
  'common.no': 'இல்லை',
  'common.dismiss': 'மூடு',
  'common.save': 'சேமி',
  'common.saving': 'சேமிக்கிறது…',
  'common.saved': 'சேமிக்கப்பட்டது',
  'common.cancel': 'ரத்து',
  'common.delete': 'நீக்கு',
  'common.edit': 'திருத்து',
  'common.create': 'உருவாக்கு',
  'common.update': 'புதுப்பி',
  'common.refresh': 'புதுப்பி',
  'common.reload': 'மீண்டும் ஏற்று',
  'common.enable': 'இயக்கு',
  'common.disable': 'முடக்கு',
  'common.enabled': 'இயக்கப்பட்டது',
  'common.disabled': 'முடக்கப்பட்டது',
  'common.active': 'செயலில்',
  'common.inactive': 'செயலற்ற',
  'common.loading': 'ஏற்றுகிறது…',
  'common.name': 'பெயர்',
  'common.description': 'விளக்கம்',
  'common.username': 'பயனர் பெயர்',
  'common.password': 'கடவுச்சொல்',
  'common.search': 'தேடு',
  'common.test': 'சோதி',
  'common.upload': 'பதிவேற்று',
  'common.download': 'பதிவிறக்கு',
  'common.start': 'தொடங்கு',
  'common.stop': 'நிறுத்து',
  'common.confirm': 'உறுதிப்படுத்து',
  'common.reset': 'மீட்டமை',
  'common.discard': 'நிராகரி',
  'common.back': 'பின்',
  'common.next': 'அடுத்து',
  'common.done': 'முடிந்தது',
  'common.status': 'நிலை',
  'common.type': 'வகை',
  'common.all': 'அனைத்தும்',
  'common.none': 'இல்லை',
  'common.optional': '(விருப்பத்தேர்வு)',
  'table.empty': 'பதிவுகள் இல்லை',
  'table.sort': 'வரிசைப்படுத்து',
  'table.sortAsc': 'ஏறு',
  'table.sortDesc': 'இறங்கு',
  'table.operator': 'செயற்குறி',
  'table.value': 'மதிப்பு',
  'table.condition': 'நிபந்தனை {n}',
  'table.filterCol': '{col} வடிகட்டு',
  'table.sortByCol': '{col} வாரியாக வரிசைப்படுத்து',
  'pager.summary': 'பக்கம் {current} / {count} · மொத்தம் {total}',
  'pager.first': 'முதல் பக்கம்',
  'pager.previous': 'முந்தைய பக்கம்',
  'pager.next': 'அடுத்த பக்கம்',
  'pager.last': 'கடைசி பக்கம்',
  'pager.goto': 'பக்கத்திற்குச் செல்',
  'pager.pageNumber': 'பக்க எண்',
  'nav.main': 'முதன்மை',
  'lang.label': 'மொழி',
  'lang.select': 'மொழியைத் தேர்ந்தெடுக்கவும்',
  'foot.built': 'கட்டப்பட்டது {date}',
  'foot.core': 'மையம் v{v}',
  'foot.commit': 'கமிட்',
  'foot.tagline': 'r450k தயாரிப்பு — மனித பொறியியலும் AI யும் ஒருவரையொருவர் எவ்வாறு நிறைவு செய்கின்றன என்பதைக் காட்டுகிறது.',
};

const DICTS = { en, ms, zh, ta };

// LANGUAGES drives the language dropdown. `label` is the language's endonym (shown in
// its own script) so it's recognizable regardless of the current UI locale.
export const LANGUAGES = [
  { code: 'en', label: 'English' },
  { code: 'ms', label: 'Melayu' },
  { code: 'zh', label: '中文' },
  { code: 'ta', label: 'தமிழ்' },
];

// Default context value works even with no provider mounted (English, no app dict).
const LangContext = createContext({ lang: 'en', messages: null });

// LangProvider sets the active locale and, optionally, an app-specific message bundle.
// `messages` is `{ en: {...}, ms: {...}, ... }` supplied by each app for its OWN strings;
// it LAYERS OVER the shared base dictionary (app key wins, then shared, then English,
// then the key). Pass a stable (module-level) object so the memo stays put.
export function LangProvider({ lang, messages = null, children }) {
  const norm = DICTS[lang] ? lang : 'en';
  const value = useMemo(() => ({ lang: norm, messages }), [norm, messages]);
  return <LangContext.Provider value={value}>{children}</LangContext.Provider>;
}

// useT returns a memoized translate function bound to the active locale, resolving
// app-locale → shared-locale → app-English → shared-English → key.
export function useT() {
  const { lang, messages } = useContext(LangContext);
  return useMemo(() => {
    const base = DICTS[lang] || en;
    const app = messages ? (messages[lang] || null) : null;
    const appEn = messages ? (messages.en || null) : null;
    return (key, vars) => {
      let s;
      if (app && app[key] != null) s = app[key];
      else if (base[key] != null) s = base[key];
      else if (appEn && appEn[key] != null) s = appEn[key];
      else if (en[key] != null) s = en[key];
      else s = key;
      if (vars) {
        for (const name of Object.keys(vars)) {
          s = s.split(`{${name}}`).join(String(vars[name]));
        }
      }
      return s;
    };
  }, [lang, messages]);
}

// normalizeLang maps a browser/stored locale (e.g. 'en-US', 'zh-Hans') to one of our
// supported codes, falling back to 'en'. Apps use it when reading the saved value.
export function normalizeLang(value) {
  const v = String(value || '').toLowerCase();
  if (v.startsWith('ms') || v.startsWith('id')) return 'ms';
  if (v.startsWith('zh')) return 'zh';
  if (v.startsWith('ta')) return 'ta';
  return 'en';
}
