// Barrel for the shared RBAC UI module. Consumers: `import { DataTable, ToastStack,
// Ico } from '@shared'` (each app aliases @shared -> frontend/shared/src via webpack).
export { Ico, icoSvg } from './icons';
export { DataTable, printable } from './DataTable';
export { ToastStack } from './Toast';
export { SideNav } from './SideNav';
export { LangProvider, useT, LANGUAGES, normalizeLang } from './i18n';
export { LanguageDropdown } from './LanguageDropdown';
export { AppFooter } from './AppFooter';
