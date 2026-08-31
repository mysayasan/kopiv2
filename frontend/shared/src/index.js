// Barrel for the shared RBAC UI module. Consumers: `import { DataTable, ToastStack,
// Ico } from '@shared'` (each app aliases @shared -> frontend/shared/src via webpack).
export { Ico, icoSvg } from './icons';
export { BrandLogo } from './BrandLogo';
export { PasswordField } from './PasswordField';
export { DataTable, printable } from './DataTable';
export { Tabs } from './Tabs';
export { CameraHero, statusTone } from './CameraHero';
export { ToastStack } from './Toast';
export { SideNav } from './SideNav';
export { LangProvider, useT, LANGUAGES, normalizeLang } from './i18n';
export { LanguageDropdown } from './LanguageDropdown';
export { AppFooter } from './AppFooter';
export { FactoryResetSection, FactoryResetDialog, FactoryResetOverlay } from './FactoryReset';
export { DeploymentPanel } from './Deployment';
export { ManualProvider, ManualLibrary, ManualDrawer, HelpButton, useManual, renderManualMarkdown } from './Manual';
export { useStickyTab, readStickyTab, writeStickyTab, clearStickyTab } from './stickyTab';
