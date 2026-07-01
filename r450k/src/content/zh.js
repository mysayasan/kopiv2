// 简体中文 (Simplified Chinese). Derives structure from en.js; only text is
// translated. (Machine-assisted — a native review pass is recommended.)
import en from './en.js';

const F = [
  { t: '以摄像头为中心的 AI 检测', b: '双轴检测规则将一种模式——存在、人群、闯入、越线、多线越线或 LPR——与来自数据驱动类别注册表的目标类别相结合。“任意”通配符会在检测到任何对象时触发。' },
  { t: '车牌识别', b: '在专用高分辨率帧上运行的二级车牌检测器 + OCR，可对任何可读车牌或仅对监视名单触发。模糊匹配可吸收 OCR 误差；车牌文本、车辆类型和颜色随告警元数据一同附带。' },
  { t: 'NVR 录制 + 压缩', b: '滚动分段缓冲，配合事件触发的 MP4 片段提取，以及低资源的 JPEG 环形缓冲模式。可选的一次性 GPU H.265/H.264 重新编码可缩小录像体积，并为任何浏览器提供即时回放转码。' },
  { t: 'WebRTC 实时画面', b: '直接将 H.264 RTP 转为 WebRTC 的实时画面，并带 MJPEG 回退。G.711 直通与 AAC→Opus 转码意味着无论摄像头音频编解码器为何都有实时声音。分页网格从 1×1 到 4×4。' },
  { t: '静态加密 + 安全擦除', b: '录像、截图和训练图像在磁盘上以 AES-256-GCM 加密。恢复出厂设置通过销毁密钥进行加密擦除；已删除的录像以多遍方式安全粉碎。' },
  { t: '训练您自己的模型', b: '从上传或告警截图构建带标注的数据集，在浏览器中绘制检测框，使用运行中的模型自动标注，在应用内于 GPU 或离线训练自定义 YOLO 模型，然后即时热切换实时检测器。' },
  { t: '统一通知信息流', b: '一个信息流贯穿 AI 检测、摄像头与主机健康以及登录安全——支持逐事件确认、带标注的截图和页内片段回放。可路由到 webhook、Telegram 或 MQTT。' },
  { t: '局域网内的机群配对', b: '经过认证的 UDP 组播发现 + 使用短时认领码的单父节点接管。节点注册机群 CA 证书并提供双向 TLS 管理通道——无需入站端口，无需云端代理。' },
];

const H = [
  { t: '发现', b: '经过认证的 ONVIF 发现与手动探测可在您的局域网中找到摄像头；已保存的设备持久化在本地 SQLite 数据库中。' },
  { t: '检测', b: '设备端 YOLO 推理针对实时画面运行您的检测规则——存在、人群、闯入、越线或车牌识别。' },
  { t: '录制', b: '持续的 NVR 录制在后台滚动进行，并在规则触发的瞬间提取 MP4 片段，加密存储于磁盘。' },
  { t: '通知', b: '告警进入统一信息流，附带标注截图和片段，并分发到您选择的 webhook、Telegram 或 MQTT 目的地。' },
];

const T = [
  { n: '最小', l: '约 1–4 路摄像头', d: '为几个摄像头提供实时画面 + 录制；AI 为原生运动检测或宽松间隔的 YOLO-nano。SQLite、CPU 软件解码、无 GPU。' },
  { n: '最佳', l: '约 6–16 路摄像头', d: '持续录制，并以默认 2 秒间隔进行实时 YOLO 检测。一块入门级 NVIDIA GPU 可提升摄像头数量并保持 AI 实时。' },
  { n: '最高', l: '20+ 路摄像头', d: 'GPU 加速检测、应用内模型训练、HEVC NVENC 压缩以及长时间留存。大规模下使用服务器数据库引擎 + Redis。' },
];

const U = [
  { t: '零售与前庭', b: '人群与闯入告警，以及用于前庭和免下车监控的车牌识别。' },
  { t: '工业与工地', b: '在上行连接较差或没有连接的场所，于边缘硬件上运行越线和区域规则。' },
  { t: '物业与周界', b: '下班后的闯入检测，配合留在本地的安全加密录制。' },
  { t: '多站点机群', b: '控制平面通过局域网接管众多边缘节点，并将实时画面回传给操作员。' },
];

const A = [
  { s: '旗舰 · 积极开发中', b: '独立的边缘摄像头与视频智能节点：ONVIF、AI 检测、NVR、WebRTC 实时画面、加密以及局域网配对。' },
  { s: '控制平面', b: '机群控制平面，负责发现、接管和管理边缘节点，并将其实时摄像头串流转发给操作员。' },
  { s: '身份', b: '共享身份与访问：JWT 认证、SSO 以及贯穿平台的基于角色的访问控制。' },
];

const SHOT_ALT = [
  '带画面内 AI 检测框的分页实时画面网格',
  '在实时画面上绘制检测区域的双轴规则编辑器',
  '带截图和确认操作的统一通知信息流',
];

const NAV = ['功能', '工作原理', '硬件', '实景展示', '应用场景', '应用'];

export default {
  ...en,
  meta: {
    title: 'r450k — 私有边缘 AI 摄像头智能',
    description:
      '本地 AI 摄像头智能：ONVIF 发现、YOLO 检测、NVR 录制、WebRTC 实时画面以及静态加密——可在小至 Raspberry Pi 的硬件上运行。',
  },
  brand: { ...en.brand, tagline: '私有边缘 AI 摄像头智能' },
  nav: en.nav.map((n, i) => ({ ...n, label: NAV[i] })),
  navCta: '浏览应用',
  hero: {
    ...en.hero,
    eyebrow: '本地部署 · 边缘 AI · 无需云端',
    titleLead: 'AI 摄像头智能，运行于',
    rotate: ['边缘。', '您的网络。', 'Raspberry Pi。', '云端之外。'],
    subtitle:
      'r450k 自动发现您的 ONVIF 摄像头，通过设备端 AI 检测关键事件，持续录制，并实时串流到浏览器。它随您的硬件而扩展——从一台监控几个摄像头的 Raspberry Pi，到运行实时 AI、覆盖数十路摄像头的 GPU 服务器。您的录像永远不会离开您的网络。',
    primaryCta: { ...en.hero.primaryCta, label: '查看功能' },
    secondaryCta: { ...en.hero.secondaryCta, label: '工作原理' },
    stats: en.hero.stats.map((s, i) => ({ ...s, label: ['从 Pi 4 起步——扩展至 GPU 服务器', '本地部署，默认私有', '低延迟实时画面'][i] })),
    note: '入门级可在 Raspberry Pi 4 上运行（几个摄像头，配合运动检测或 nano 模型 AI）。实时多摄像头检测与应用内训练则可扩展至迷你 PC 和 NVIDIA GPU。',
  },
  features: {
    ...en.features,
    kicker: '核心能力',
    title: '边缘 AI 摄像头节点所需的一切。',
    lead: '检测、录制、实时画面、安全与机群管理——专为在小型设备上、完全在您自己的网络中运行而打造。',
    items: en.features.items.map((it, i) => ({ ...it, title: F[i].t, body: F[i].b })),
  },
  how: {
    ...en.how,
    kicker: '工作原理',
    title: '四步，从裸摄像头到可执行的告警。',
    steps: en.how.steps.map((s, i) => ({ ...s, title: H[i].t, body: H[i].b })),
  },
  tiers: {
    ...en.tiers,
    kicker: '硬件',
    title: '随您的硬件而扩展。',
    lead: '同一应用可从口袋大小的设备运行到 GPU 服务器——由您选择级别。（摄像头数量为粗略估计；内置容量估算器会为您的主机测算真实数值。）',
    recommended: '推荐',
    items: en.tiers.items.map((it, i) => ({ ...it, name: T[i].n, load: T[i].l, detail: T[i].d })),
  },
  showcase: {
    ...en.showcase,
    kicker: '实际体验',
    title: '为操作员而非仅为管理员打造的控制台。',
    lead: '实时多摄像头网格、绘制在真实画面上的双轴检测规则，以及一个统一的告警信息流——全部在浏览器中。',
    tabs: { live: '实时画面', rules: '检测规则', notifications: '通知' },
    shots: en.showcase.shots.map((s, i) => ({ ...s, alt: SHOT_ALT[i] })),
  },
  useCases: {
    ...en.useCases,
    kicker: '应用场景',
    title: '为无法将视频上传云端的场所而打造。',
    items: en.useCases.items.map((it, i) => ({ ...it, title: U[i].t, body: U[i].b })),
  },
  apps: {
    ...en.apps,
    kicker: '平台',
    title: '一个平台，三个应用。',
    subtitle: 'r450k 是一个模块化平台。mymatasan 是边缘监控节点；控制平面负责身份与机群管理。',
    available: '可用',
    platform: '平台',
    items: en.apps.items.map((it, i) => ({ ...it, status: A[i].s, body: A[i].b })),
  },
  finalCta: {
    ...en.finalCta,
    title: '让您的视频——以及您的智能——留在自己的网络中。',
    body: 'r450k 将云端级的摄像头 AI 带到您已拥有的硬件上，让隐私成为默认而非升级项。',
    cta: { ...en.finalCta.cta, label: '探索功能' },
  },
  contact: {
    ...en.contact,
    fabLabel: '联系',
    title: '联系我',
    blurb: '在下方留言，它会直接发送到我的 Telegram。',
    namePlaceholder: '您的姓名（可选）',
    messagePlaceholder: '我能帮您什么？',
    send: '发送消息',
    sending: '发送中…',
    sent: '谢谢——您的消息正在发送到我的 Telegram。',
    again: '再发一条',
    errRequired: '请输入消息。',
    errUnreachable: '无法连接消息端点。（本地开发时，请运行 Worker——参见 README。）',
    errGeneric: '出了点问题。请重试。',
  },
  footer: {
    ...en.footer,
    tagline: 'r450k · 私有边缘 AI 摄像头智能',
    note: '本地运行。您的录像永远不会离开您的网络。',
    rights: '保留所有权利。',
    columns: en.footer.columns.map((c) => ({ ...c, heading: '产品', links: c.links.map((l, j) => ({ ...l, label: ['功能', '工作原理', '应用场景', '应用'][j] })) })),
  },
};
