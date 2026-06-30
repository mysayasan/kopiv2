// All site copy lives here so content edits never touch layout code.
// Sourced from apps/mymatasan/README.md and the platform READMEs.

// Canonical site origin (no trailing slash). Used for <link rel="canonical">,
// og:url, robots.txt, and sitemap.xml. Change here if the domain ever moves.
export const siteUrl = 'https://r450k.com';

export const brand = {
  name: 'r450k',
  tagline: 'Private edge AI camera intelligence',
};

export const nav = [
  { label: 'Features', href: '#features' },
  { label: 'How it works', href: '#how' },
  { label: 'Hardware', href: '#tiers' },
  { label: 'Showcase', href: '#showcase' },
  { label: 'Use cases', href: '#use-cases' },
  { label: 'Apps', href: '#apps' },
];

export const hero = {
  eyebrow: 'On-prem · Edge AI · No cloud required',
  title: 'AI camera intelligence that runs where your cameras are.',
  titleLead: 'AI camera intelligence that runs',
  rotate: ['at the edge.', 'on your network.', 'on a Raspberry Pi.', 'without the cloud.'],
  subtitle:
    'r450k discovers your ONVIF cameras, detects what matters with on-device AI, records continuously, and streams live to the browser. It scales with your hardware — from a single Raspberry Pi watching a couple of cameras to a GPU server running real-time AI across dozens. Your footage never leaves your network.',
  primaryCta: { label: 'See the features', href: '#features' },
  secondaryCta: { label: 'How it works', href: '#how' },
  stats: [
    { value: 'Raspberry Pi', label: 'Starts on a Pi 4 — scales to GPU servers' },
    { value: '100%', label: 'On-prem, private by default' },
    { value: 'WebRTC', label: 'Low-latency live view' },
  ],
  note: 'Entry tier runs on a Raspberry Pi 4 (a couple of cameras with motion or nano-model AI). Real-time, multi-camera detection and in-app training scale up to mini-PCs and NVIDIA GPUs.',
};

export const features = [
  {
    icon: 'eye',
    title: 'Camera-first AI detection',
    body: 'Two-axis detection rules combine a mode — presence, crowd, intrusion, line crossing, multi-line crossing, or LPR — with target classes from a data-driven class registry. An "Anything" wildcard fires on any detected object.',
  },
  {
    icon: 'car',
    title: 'License-plate recognition',
    body: 'A second-stage plate detector + OCR on a dedicated high-res frame fires on any readable plate or only a watchlist. Fuzzy matching absorbs OCR errors; plate text, vehicle type, and color ride the alert metadata.',
  },
  {
    icon: 'record',
    title: 'NVR recording + compression',
    body: 'Rolling segment buffer with event-triggered MP4 clip extraction, plus a low-resource JPEG ring-buffer mode. Optional one-pass GPU H.265/H.264 re-encode shrinks footage with on-the-fly playback transcode for any browser.',
  },
  {
    icon: 'play',
    title: 'WebRTC live view',
    body: 'Direct H.264 RTP-to-WebRTC live view with MJPEG fallback. G.711 pass-through and AAC→Opus transcoding mean live sound regardless of the camera audio codec. Paged grids from 1×1 up to 4×4.',
  },
  {
    icon: 'lock',
    title: 'Encryption at rest + secure wipe',
    body: 'Recordings, snapshots, and training images are AES-256-GCM encrypted on disk. Factory reset crypto-erases by destroying the key; deleted footage is securely multi-pass shredded.',
  },
  {
    icon: 'brain',
    title: 'Train your own models',
    body: 'Build labeled datasets from uploads or alert snapshots, draw boxes in-browser, auto-label with the running model, train a custom YOLO model in-app on a GPU or offline, then hot-swap the live detector.',
  },
  {
    icon: 'bell',
    title: 'Unified notification feed',
    body: 'One feed across AI detection, camera and machine health, and login security — with per-event acknowledge, annotated screenshots, and in-page clip playback. Route to webhook, Telegram, or MQTT.',
  },
  {
    icon: 'link',
    title: 'Fleet pairing over the LAN',
    body: 'Authenticated UDP multicast discovery + single-parent adoption with a short-lived claim code. Nodes enroll for a fleet-CA certificate and serve a mutual-TLS management channel — no inbound ports, no cloud broker.',
  },
];

export const how = {
  title: 'From bare cameras to actionable alerts in four steps.',
  steps: [
    {
      n: '01',
      title: 'Discover',
      body: 'Authenticated ONVIF discovery and manual probe find cameras on your LAN; saved devices persist in a local SQLite database.',
    },
    {
      n: '02',
      title: 'Detect',
      body: 'On-device YOLO inference runs your detection rules — presence, crowd, intrusion, line crossing, or plate recognition — against the live frames.',
    },
    {
      n: '03',
      title: 'Record',
      body: 'Continuous NVR recording rolls in the background and extracts an MP4 clip the moment a rule fires, encrypted on disk.',
    },
    {
      n: '04',
      title: 'Notify',
      body: 'Alerts land in the unified feed with an annotated snapshot and clip, and fan out to webhook, Telegram, or MQTT destinations you choose.',
    },
  ],
};

export const showcase = {
  title: 'A console built for operators, not just admins.',
  lead: 'Live multi-camera grids, two-axis detection rules drawn on the real frame, and one unified alert feed — all in the browser.',
  shots: [
    {
      key: 'live',
      src: '/screenshots/live-view.webp',
      alt: 'Paged live-view grid with on-frame AI detection boxes',
      url: 'mymatasan.local/live',
    },
    {
      key: 'rules',
      src: '/screenshots/rule-editor.webp',
      alt: 'Two-axis rule editor with a detection zone drawn on the live frame',
      url: 'mymatasan.local/vision',
    },
    {
      key: 'notifications',
      src: '/screenshots/notifications.webp',
      alt: 'Unified notification feed with snapshots and acknowledge actions',
      url: 'mymatasan.local/notifications',
    },
  ],
};

export const useCases = {
  title: 'Built for places that can’t send video to the cloud.',
  items: [
    { title: 'Retail & forecourts', body: 'Crowd and intrusion alerts, plus license-plate recognition for forecourt and drive-through monitoring.' },
    { title: 'Industrial & sites', body: 'Line-crossing and zone rules on edge hardware in places with poor or no upstream connectivity.' },
    { title: 'Property & perimeter', body: 'After-hours intrusion detection with secure, encrypted recording that stays on-site.' },
    { title: 'Multi-site fleets', body: 'A control plane adopts many edge nodes over the LAN and relays live view back to operators.' },
  ],
};

export const apps = {
  title: 'One platform, three apps.',
  subtitle: 'r450k is a modular platform. mymatasan is the edge surveillance node; the control plane handles identity and fleet management.',
  items: [
    {
      name: 'mymatasan',
      status: 'Flagship · in active development',
      available: true,
      body: 'The standalone edge camera & video-intelligence node: ONVIF, AI detection, NVR, WebRTC live view, encryption, and LAN pairing.',
    },
    {
      name: 'myseliasan',
      status: 'Control plane',
      available: false,
      body: 'The fleet control plane that discovers, adopts, and manages edge nodes, and relays their live camera streams to operators.',
    },
    {
      name: 'myidsan',
      status: 'Identity',
      available: false,
      body: 'Shared identity and access: JWT auth, SSO, and role-based access control across the platform.',
    },
  ],
};

export const tiers = {
  title: 'Scales with your hardware.',
  lead: 'The same app runs from a pocket-sized appliance to a GPU server — you choose the tier. (Camera counts are rough; the built-in capacity estimator measures the real number for your host.)',
  items: [
    {
      name: 'Minimal',
      spec: 'Raspberry Pi 4/5 · 4 cores · 2–4 GB',
      load: '~1–4 cameras',
      detail: 'Live view + recording for a couple of cameras; AI is native motion or YOLO-nano at a relaxed interval. SQLite, CPU software decode, no GPU.',
    },
    {
      name: 'Optimal',
      spec: 'Mini-PC / NUC · 6–8 cores · 8–16 GB',
      load: '~6–16 cameras',
      detail: 'Continuous recording with real-time YOLO detection at the 2 s default. An entry NVIDIA GPU lifts camera count and keeps AI real-time.',
      featured: true,
    },
    {
      name: 'Maximum',
      spec: 'Server · 12+ cores · 32 GB+ · NVIDIA GPU',
      load: '20+ cameras',
      detail: 'GPU-accelerated detection, in-app model training, HEVC NVENC compression, and long retention. Server DB engine + Redis at scale.',
    },
  ],
};

export const finalCta = {
  title: 'Keep your video — and your intelligence — on your own network.',
  body: 'r450k brings cloud-grade camera AI to hardware you already own, with privacy as the default rather than an upgrade.',
  cta: { label: 'Explore the features', href: '#features' },
};

export const contact = {
  enabled: true,
  // Worker route that relays the form to the Telegram bot (same-origin).
  // See r450k/worker/index.js + README.
  endpoint: '/api/contact',
  title: 'Get in touch',
  blurb: 'Send a message below and it lands straight in my Telegram.',
};

export const footer = {
  tagline: 'r450k · Private edge AI camera intelligence',
  note: 'Runs on-prem. Your footage never leaves your network.',
  columns: [
    {
      heading: 'Product',
      links: [
        { label: 'Features', href: '#features' },
        { label: 'How it works', href: '#how' },
        { label: 'Use cases', href: '#use-cases' },
        { label: 'Apps', href: '#apps' },
      ],
    },
  ],
};
