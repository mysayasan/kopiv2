// Bahasa Melayu. Derives structure from en.js; only text is translated.
// (Machine-assisted — a native review pass is recommended.)
import en from './en.js';

const F = [
  { t: 'Pengesanan AI mengutamakan kamera', b: 'Peraturan pengesanan dua paksi menggabungkan mod — kehadiran, orang ramai, pencerobohan, lintasan garis, lintasan berbilang garis, atau LPR — dengan kelas sasaran daripada daftar kelas dipacu data. Wildcard "Apa sahaja" tercetus pada mana-mana objek yang dikesan.' },
  { t: 'Pengecaman plat nombor', b: 'Pengesan plat peringkat kedua + OCR pada bingkai resolusi tinggi khusus tercetus pada mana-mana plat yang boleh dibaca atau hanya senarai pantau. Padanan kabur menyerap ralat OCR; teks plat, jenis kenderaan, dan warna disertakan dalam metadata amaran.' },
  { t: 'Rakaman NVR + pemampatan', b: 'Penimbal segmen bergolek dengan pengekstrakan klip MP4 dicetus peristiwa, serta mod penimbal cincin JPEG sumber rendah. Pilihan pengekodan semula GPU H.265/H.264 sekali lalu mengecilkan rakaman dengan transkod main balik serta-merta untuk mana-mana pelayar.' },
  { t: 'Paparan langsung WebRTC', b: 'Paparan langsung RTP H.264 ke WebRTC secara terus dengan sandaran MJPEG. Laluan terus G.711 dan transkod AAC→Opus bermakna bunyi langsung tanpa mengira kodek audio kamera. Grid berhalaman dari 1×1 hingga 4×4.' },
  { t: 'Penyulitan semasa rehat + padam selamat', b: 'Rakaman, tangkapan skrin, dan imej latihan disulitkan AES-256-GCM pada cakera. Tetapan semula kilang melakukan padam-kripto dengan memusnahkan kunci; rakaman yang dipadam dicincang berbilang-laluan dengan selamat.' },
  { t: 'Latih model anda sendiri', b: 'Bina set data berlabel daripada muat naik atau tangkapan amaran, lukis kotak dalam pelayar, auto-label dengan model yang berjalan, latih model YOLO tersuai dalam aplikasi pada GPU atau luar talian, kemudian tukar pengesan langsung serta-merta.' },
  { t: 'Suapan pemberitahuan bersepadu', b: 'Satu suapan merentas pengesanan AI, kesihatan kamera dan mesin, dan keselamatan log masuk — dengan pengesahan setiap peristiwa, tangkapan skrin beranotasi, dan main balik klip dalam halaman. Halakan ke webhook, Telegram, atau MQTT.' },
  { t: 'Pemadanan armada melalui LAN', b: 'Penemuan multicast UDP disahkan + penggunaan induk tunggal dengan kod tuntutan jangka pendek. Nod mendaftar untuk sijil CA-armada dan menyediakan saluran pengurusan TLS bersama — tiada port masuk, tiada broker awan.' },
];

const H = [
  { t: 'Temui', b: 'Penemuan ONVIF disahkan dan siasatan manual mencari kamera pada LAN anda; peranti disimpan kekal dalam pangkalan data SQLite tempatan.' },
  { t: 'Kesan', b: 'Inferens YOLO pada peranti menjalankan peraturan pengesanan anda — kehadiran, orang ramai, pencerobohan, lintasan garis, atau pengecaman plat — terhadap bingkai langsung.' },
  { t: 'Rakam', b: 'Rakaman NVR berterusan berjalan di latar belakang dan mengekstrak klip MP4 sebaik sahaja peraturan tercetus, disulitkan pada cakera.' },
  { t: 'Maklum', b: 'Amaran tiba dalam suapan bersepadu dengan tangkapan skrin dan klip beranotasi, dan disebarkan ke destinasi webhook, Telegram, atau MQTT pilihan anda.' },
];

const T = [
  { n: 'Minimum', l: '~1–4 kamera', d: 'Paparan langsung + rakaman untuk beberapa kamera; AI ialah gerakan asli atau YOLO-nano pada selang santai. SQLite, nyahkod perisian CPU, tiada GPU.' },
  { n: 'Optimum', l: '~6–16 kamera', d: 'Rakaman berterusan dengan pengesanan YOLO masa nyata pada lalai 2 saat. GPU NVIDIA permulaan meningkatkan bilangan kamera dan mengekalkan AI masa nyata.' },
  { n: 'Maksimum', l: '20+ kamera', d: 'Pengesanan dipecut GPU, latihan model dalam aplikasi, pemampatan HEVC NVENC, dan pengekalan panjang. Enjin DB pelayan + Redis pada skala besar.' },
];

const U = [
  { t: 'Runcit & laman hadapan', b: 'Amaran orang ramai dan pencerobohan, serta pengecaman plat nombor untuk pemantauan laman hadapan dan pandu lalu.' },
  { t: 'Industri & tapak', b: 'Peraturan lintasan garis dan zon pada perkakasan edge di tempat dengan sambungan huluan lemah atau tiada.' },
  { t: 'Hartanah & perimeter', b: 'Pengesanan pencerobohan selepas waktu kerja dengan rakaman selamat tersulit yang kekal di tapak.' },
  { t: 'Armada berbilang tapak', b: 'Satah kawalan menggunakan banyak nod edge melalui LAN dan menyampaikan paparan langsung kembali kepada operator.' },
];

const A = [
  { s: 'Perdana · dalam pembangunan aktif', b: 'Nod kamera & risikan video edge berdiri sendiri: ONVIF, pengesanan AI, NVR, paparan langsung WebRTC, penyulitan, dan pemadanan LAN.' },
  { s: 'Satah kawalan', b: 'Satah kawalan armada yang menemui, mengguna, dan mengurus nod edge, serta menyampaikan strim kamera langsung mereka kepada operator.' },
  { s: 'Identiti', b: 'Identiti dan akses dikongsi: pengesahan JWT, SSO, dan kawalan akses berasaskan peranan merentas platform.' },
];

const SHOT_ALT = [
  'Grid paparan langsung berhalaman dengan kotak pengesanan AI pada bingkai',
  'Editor peraturan dua paksi dengan zon pengesanan dilukis pada bingkai langsung',
  'Suapan pemberitahuan bersepadu dengan tangkapan skrin dan tindakan pengesahan',
];

const NAV = ['Ciri', 'Cara ia berfungsi', 'Perkakasan', 'Pameran', 'Kegunaan', 'Aplikasi'];

export default {
  ...en,
  meta: {
    title: 'r450k — Kecerdasan kamera AI edge yang peribadi',
    description:
      'Kecerdasan kamera AI di premis: penemuan ONVIF, pengesanan YOLO, rakaman NVR, paparan langsung WebRTC, dan penyulitan semasa rehat — berjalan pada perkakasan sekecil Raspberry Pi.',
  },
  brand: { ...en.brand, tagline: 'Kecerdasan kamera AI edge yang peribadi' },
  nav: en.nav.map((n, i) => ({ ...n, label: NAV[i] })),
  navCta: 'Terokai aplikasi',
  hero: {
    ...en.hero,
    eyebrow: 'Di premis · AI Edge · Tanpa awan',
    titleLead: 'Kecerdasan kamera AI yang berjalan',
    rotate: ['di edge.', 'pada rangkaian anda.', 'pada Raspberry Pi.', 'tanpa awan.'],
    subtitle:
      'r450k menemui kamera ONVIF anda, mengesan perkara penting dengan AI pada peranti, merakam secara berterusan, dan menstrim secara langsung ke pelayar. Ia berskala dengan perkakasan anda — daripada satu Raspberry Pi yang memantau beberapa kamera kepada pelayan GPU yang menjalankan AI masa nyata merentas berpuluh-puluh. Rakaman anda tidak pernah meninggalkan rangkaian anda.',
    primaryCta: { ...en.hero.primaryCta, label: 'Lihat ciri' },
    secondaryCta: { ...en.hero.secondaryCta, label: 'Cara ia berfungsi' },
    stats: en.hero.stats.map((s, i) => ({ ...s, label: ['Bermula pada Pi 4 — berskala ke pelayan GPU', 'Di premis, peribadi secara lalai', 'Paparan langsung kependaman rendah'][i] })),
    note: 'Peringkat permulaan berjalan pada Raspberry Pi 4 (beberapa kamera dengan AI gerakan atau model-nano). Pengesanan masa nyata berbilang kamera dan latihan dalam aplikasi berskala ke mini-PC dan GPU NVIDIA.',
  },
  features: {
    ...en.features,
    kicker: 'Keupayaan',
    title: 'Segala yang diperlukan oleh nod kamera AI edge.',
    lead: 'Pengesanan, rakaman, paparan langsung, keselamatan, dan pengurusan armada — dibina untuk berjalan pada peranti kecil, sepenuhnya pada rangkaian anda sendiri.',
    items: en.features.items.map((it, i) => ({ ...it, title: F[i].t, body: F[i].b })),
  },
  how: {
    ...en.how,
    kicker: 'Cara ia berfungsi',
    title: 'Daripada kamera kosong kepada amaran boleh tindak dalam empat langkah.',
    steps: en.how.steps.map((s, i) => ({ ...s, title: H[i].t, body: H[i].b })),
  },
  tiers: {
    ...en.tiers,
    kicker: 'Perkakasan',
    title: 'Berskala dengan perkakasan anda.',
    lead: 'Aplikasi yang sama berjalan daripada peranti sebesar poket kepada pelayan GPU — anda pilih peringkatnya. (Bilangan kamera adalah anggaran; penganggar kapasiti terbina mengukur angka sebenar untuk hos anda.)',
    recommended: 'Disyorkan',
    items: en.tiers.items.map((it, i) => ({ ...it, name: T[i].n, load: T[i].l, detail: T[i].d })),
  },
  showcase: {
    ...en.showcase,
    kicker: 'Lihat ia beraksi',
    title: 'Konsol dibina untuk operator, bukan sekadar pentadbir.',
    lead: 'Grid berbilang kamera langsung, peraturan pengesanan dua paksi dilukis pada bingkai sebenar, dan satu suapan amaran bersepadu — semuanya dalam pelayar.',
    tabs: { live: 'Paparan langsung', rules: 'Peraturan pengesanan', notifications: 'Pemberitahuan' },
    shots: en.showcase.shots.map((s, i) => ({ ...s, alt: SHOT_ALT[i] })),
  },
  useCases: {
    ...en.useCases,
    kicker: 'Kegunaan',
    title: 'Dibina untuk tempat yang tidak boleh menghantar video ke awan.',
    items: en.useCases.items.map((it, i) => ({ ...it, title: U[i].t, body: U[i].b })),
  },
  apps: {
    ...en.apps,
    kicker: 'Platform',
    title: 'Satu platform, tiga aplikasi.',
    subtitle: 'r450k ialah platform bermodul. mymatasan ialah nod pengawasan edge; satah kawalan mengendalikan identiti dan pengurusan armada.',
    available: 'Tersedia',
    platform: 'Platform',
    items: en.apps.items.map((it, i) => ({ ...it, status: A[i].s, body: A[i].b })),
  },
  finalCta: {
    ...en.finalCta,
    title: 'Simpan video anda — dan risikan anda — pada rangkaian anda sendiri.',
    body: 'r450k membawa AI kamera bertaraf awan ke perkakasan yang anda sudah miliki, dengan privasi sebagai lalai dan bukan naik taraf.',
    cta: { ...en.finalCta.cta, label: 'Terokai ciri' },
  },
  contact: {
    ...en.contact,
    fabLabel: 'Hubungi',
    title: 'Hubungi saya',
    blurb: 'Hantar mesej di bawah dan ia terus sampai ke Telegram saya.',
    namePlaceholder: 'Nama anda (pilihan)',
    messagePlaceholder: 'Bagaimana saya boleh bantu?',
    send: 'Hantar mesej',
    sending: 'Menghantar…',
    sent: 'Terima kasih — mesej anda dalam perjalanan ke Telegram saya.',
    again: 'Hantar satu lagi',
    errRequired: 'Sila masukkan mesej.',
    errUnreachable: 'Titik akhir mesej tidak dapat dihubungi. (Dalam pembangunan tempatan, jalankan Worker — lihat README.)',
    errGeneric: 'Sesuatu tidak kena. Sila cuba lagi.',
  },
  footer: {
    ...en.footer,
    tagline: 'r450k · Kecerdasan kamera AI edge yang peribadi',
    note: 'Berjalan di premis. Rakaman anda tidak pernah meninggalkan rangkaian anda.',
    rights: 'Hak cipta terpelihara.',
    columns: en.footer.columns.map((c) => ({ ...c, heading: 'Produk', links: c.links.map((l, j) => ({ ...l, label: ['Ciri', 'Cara ia berfungsi', 'Kegunaan', 'Aplikasi'][j] })) })),
  },
};
