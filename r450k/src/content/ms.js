// Bahasa Melayu. Derives structure from en.js; only text is translated.
// (Machine-assisted — a native review pass is recommended.)
import en from './en.js';

const F = [
  { t: 'Pengesanan AI mengutamakan kamera', b: 'Peraturan dua paksi memadankan mod — kehadiran, orang ramai, pencerobohan, lintasan garis, lintasan berbilang garis, atau LPR — dengan kelas sasaran daripada daftar dipacu data, diskopkan kepada berbilang zon pengesanan setiap peraturan. Api dan asap disertakan sebagai kelas peristiwa utama, dan wildcard "Apa sahaja" tercetus pada mana-mana objek yang dikesan.' },
  { t: 'Pengecaman plat nombor', b: 'Pengesan plat peringkat kedua + OCR pada bingkai resolusi tinggi khusus tercetus pada mana-mana plat yang boleh dibaca atau hanya senarai pantau. Padanan kabur menyerap ralat OCR; teks plat, jenis kenderaan, dan warna disertakan dalam metadata amaran.' },
  { t: 'Pengecaman wajah', b: 'Daftarkan orang yang anda kenali, dan satu pengesanan menjadi amaran bernama. Pada nod edge, wajah dijajarkan, dibenamkan, dan dipadankan dengan galeri peribadi anda — jadi “orang dikesan” bertukar menjadi “wajah dikenali di pintu hadapan.” Pendaftaran dan pemadanan berjalan sepenuhnya di premis: tiada perkhidmatan awan, tiada wajah pernah meninggalkan rangkaian anda.' },
  { t: 'Rakaman NVR + pemampatan', b: 'Penimbal segmen bergolek dengan pengekstrakan klip MP4 dicetus peristiwa, serta mod penimbal cincin JPEG sumber rendah. Pilihan pengekodan semula GPU H.265/H.264 sekali lalu mengecilkan rakaman dengan transkod main balik serta-merta untuk mana-mana pelayar.' },
  { t: 'Paparan langsung dengan cakap balik', b: 'Paparan langsung RTP H.264 ke WebRTC secara terus dengan sandaran MJPEG dan bunyi langsung pada mana-mana kodek kamera (laluan terus G.711, transkod AAC→Opus). Cakap balik audio dua hala dan PTZ tekan-dan-tahan membolehkan operator bertindak balas, bukan sekadar menonton — merentas grid berhalaman dari 1×1 hingga 4×4.' },
  { t: 'Penderia, telemetri & penggerakan selamat', b: 'Idea yang sama seperti NVR, tetapi untuk segala yang bukan kamera: sesentuh pintu, gerakan, suhu, asap, kebocoran, meter kuasa dan penyongsang Modbus/SunSpec, melalui broker MQTT terbenam — serta imbasan LAN baca-sahaja pilihan yang mencari apa yang sudah ada pada rangkaian. Stor bertapis-hingar mengekalkan sejarah kecil, peraturan ambang dan korelasi membangkitkan amaran, dan adegan, jadual serta kanvas aliran visual mengautomasikan tindak balas. Setiap arahan kembali ke peranti dipaksa melalui satu titik sekatan yang terbatas, terhad kadar, dan diaudit sepenuhnya.' },
  { t: 'Kawalan akses di pintu', b: 'Pembaca lencana bertutur OSDP melalui RS-485 atau IP, dan setiap leretan lencana diputuskan pada perkakas itu sendiri terhadap orang, kumpulan, jadual dan cuti — jadi sambungan terputus tidak pernah bermakna pintu yang tidak mahu terbuka. Sekatan seluruh tapak, PIN duress yang membuka pintu sambil senyap membangkitkan penggera, dan log aktiviti tambah-sahaja disertakan sebagai standard, dan pengunci pintu hanya tercetus melalui satu titik sekatan yang diaudit.' },
  { t: 'Kaitkan kamera, penderia dan pintu', b: 'Peraturan yang tidak boleh ditulis oleh satu perkakas sahaja: gerakan pada kamera DAN pintu terbuka DAN tiada lencana diterima, dalam tetingkap yang sama, di tapak yang sama. Satah kawalan bersedia pada isyarat pertama, menunggu tempoh tangguh untuk penjelasan yang tidak bersalah, dan barulah membangkitkan satu insiden berkait, bukan tiga amaran yang tidak berkaitan.' },
  { t: 'Penyulitan, sandaran & pemulihan', b: 'Rakaman, tangkapan skrin, dan imej latihan disulitkan AES-256-GCM pada cakera; tetapan semula kilang melakukan padam-kripto dengan memusnahkan kunci dan mencincang rakaman terpadam berbilang-laluan. Kunci dibalut oleh stor kunci OS (DPAPI / systemd-creds) atau frasa laluan mudah alih dengan eskro pemulihan boleh eksport — dan .mmbackup tersulit frasa laluan memindahkan kamera, peraturan, dan tetapan antara hos.' },
  { t: 'Ajar ia kemahiran baharu', b: 'Tanpa jargon ML atau pengurusan set data. Bestari berpandu mengajar kamera kemahiran baharu — namakannya, pilih matlamat (kenali objek baharu, bezakan baik daripada rosak, atau kesan apa-apa yang luar biasa), tunjuk beberapa contoh, semak ketepatan, dan hidupkannya. Ia membina dan melatih model tersuai sebenar di sebalik tabir, kemudian menukar pengesan langsung serta-merta.' },
  { t: 'Suapan pemberitahuan bersepadu', b: 'Satu suapan merentas pengesanan AI, kesihatan kamera dan mesin, dan keselamatan log masuk — dengan pengesahan setiap peristiwa, tangkapan skrin beranotasi, dan main balik klip dalam halaman. Tapis suapan kepada satu kamera sumber untuk mengasingkan peristiwa satu paparan. Halakan ke webhook, Telegram, atau MQTT.' },
  { t: 'Pemadanan armada melalui LAN', b: 'Penemuan multicast UDP disahkan + penggunaan induk tunggal dengan kod tuntutan jangka pendek. Nod mendaftar untuk sijil CA-armada dan menyediakan saluran pengurusan TLS bersama — tiada port masuk, tiada broker awan. Sijil setiap nod diperbaharui secara automatik mengikut jadual berpagar-operator, jadi kepercayaan armada kekal terkini dan tiada nod terlupa yang luput.' },
  { t: 'Peta armada & pelan lantai', b: 'Satah kawalan meletakkan seluruh harta anda pada peta — tapak dan bangunan dipin mengikut lokasi, dengan setiap nod edge dan kameranya dilapis di atasnya. Masuk ke sebuah bangunan untuk mereka bentuk pelan lantainya dalam aplikasi, letakkan setiap kamera pada pelan dengan kon liputan medan pandangan (FOV), dan lihat kamera satu bangunan diagregatkan merentas beberapa nod. Klik mana-mana kamera untuk mencari lokasinya atau membuka paparan langsungnya terus dari peta.' },
  { t: 'Penganalisis yang membaca armada untuk anda', b: 'Ringkasan harian — dan mingguan jika mahu — membaca seluruh harta anda dan melaporkan apa yang berubah: ayunan jumlah peristiwa, nod yang menyimpang daripada garis dasarnya sendiri, gangguan, sijil hampir luput, sumber bising, aktiviti audit sensitif, malah cadangan peraturan untuk aktiviti selepas waktu kerja yang belum diliputi. Setiap angka dikira dalam Go biasa, jadi penemuan ringkasan tidak pernah berupa halusinasi. Model bahasa pilihan — titik akhir anda sendiri, atau sidecar terselia yang boleh dibawa masuk dengan pemacu USB — menambah sembang "tanya armada" yang memetik rekod yang digunakannya dan menjawab "bagaimana saya…" terus daripada manual terbina. Ia berjalan pada perkakasan anda atau tidak langsung.' },
  { t: 'Papan pemuka analitik', b: 'Suapan peristiwa bersepadu menjadi wawasan — jubin KPI, pengesanan mengikut masa, dan pecahan mengikut kategori dan kamera dengan pemilih julat dan auto-segar. Ia melangkaui carta: peta haba aktiviti mengikut jam dan hari minggu, jalur julat dijangka yang menandakan apabila kamera menyimpang daripada corak normalnya, amaran anomali lonjakan dan "senyap luar biasa", kad skor kebolehpercayaan setiap kamera, dan nisbah bising pemberitahuan. Pengagregatan berjalan di pihak pelayan, jadi ia berfungsi pada SQLite atau enjin pangkalan data penuh.' },
  { t: 'Carian Objek', b: 'Cari rakaman anda mengikut apa yang benar-benar dilihat kamera. Setiap pengesanan dikumpulkan pada garis masa boleh cari — tapis mengikut kamera, julat tarikh, satu atau lebih jenis objek sekaligus, dan keyakinan, kemudian lompat terus ke saat tepat dalam rakaman, dengan objek dikotakkan pada lakaran kecil pratonton. Eksport hasil ke CSV atau PDF. Ia memanfaatkan output pengesan langsung, jadi tiada laluan video kedua.' },
  { t: 'Identiti perusahaan, MFA & audit', b: 'Satu log masuk merentas setiap aplikasi, daripada satu binari Go tulen yang tidak pernah meninggalkan intranet anda. Akaun tempatan bersama LDAP / Active Directory, SSO desktop Kerberos SPNEGO dan pembekal OIDC generik, dengan pemetaan kumpulan-ke-peranan dan kawalan akses setiap aplikasi dan setiap halaman. TOTP dan kunci keselamatan WebAuthn, dasar kata laluan, kuncian dan pengesahan semula langkah-naik menjaga pintu hadapan; jejak audit tidak boleh diubah, pentadbiran sesi langsung dan sandaran/pemulihan tersulit frasa laluan mengekalkan kebertanggungjawaban.' },
  { t: 'Pasang dan jalan sendiri', b: 'Bestari larian pertama memandu persediaan dari mula ke akhir dalam setiap aplikasi, penganggar kapasiti menyaiz hos anda, dan pemantauan kesihatan mesin memulih sendiri — menulis ganti rakaman terlama sebelum cakera penuh. Setiap aplikasi turut membawa manual pengguna empat bahasa yang boleh dicetak, dikompil ke dalam binari dan boleh dibaca luar talian dari skrin log masuk. ffmpeg, masa jalan AI Python, dan kemas kini aplikasi semuanya dipasang dari dalam aplikasi.' },
];

const H = [
  { t: 'Temui', b: 'Penemuan ONVIF disahkan dan siasatan manual mencari kamera pada LAN anda, tetingkap pendaftaran atau imbasan rangkaian baca-sahaja mencari penderia, dan pembaca OSDP mengumumkan diri pada bas. Peranti disimpan kekal dalam pangkalan data SQLite tempatan.' },
  { t: 'Kesan', b: 'Inferens YOLO pada peranti menjalankan peraturan pengesanan anda — kehadiran, orang ramai, pencerobohan, lintasan garis, atau pengecaman plat — terhadap bingkai langsung, manakala ambang penderia dan keputusan lencana dinilai pada perkakas itu sendiri.' },
  { t: 'Rakam', b: 'Rakaman NVR berterusan berjalan di latar belakang dan mengekstrak klip MP4 sebaik sahaja peraturan tercetus, disulitkan pada cakera; telemetri dan setiap keputusan akses masuk ke sejarah tambah-sahaja mereka sendiri.' },
  { t: 'Maklum', b: 'Amaran tiba dalam suapan bersepadu dengan tangkapan skrin dan klip beranotasi, dikaitkan merentas kamera, penderia dan pintu di satah kawalan, dan disebarkan ke destinasi webhook, Telegram, atau MQTT pilihan anda.' },
];

const T = [
  { n: 'Minimum', l: '~1–4 kamera', d: 'Paparan langsung + rakaman untuk beberapa kamera; AI ialah gerakan asli atau YOLO-nano pada selang santai. SQLite, nyahkod perisian CPU, tiada GPU.' },
  { n: 'Optimum', l: '~6–16 kamera', d: 'Rakaman berterusan dengan pengesanan YOLO masa nyata pada lalai 2 saat. GPU NVIDIA permulaan meningkatkan bilangan kamera dan mengekalkan AI masa nyata.' },
  { n: 'Maksimum', l: '20+ kamera', d: 'Pengesanan dipecut GPU, latihan model dalam aplikasi, pemampatan HEVC NVENC, dan pengekalan panjang. Enjin DB pelayan + Redis pada skala besar.' },
];

const U = [
  { t: 'Pembuatan & QA', b: 'Latih model tersuai untuk mengesan kecacatan produk, bahagian hilang, atau ketiadaan PPE pada barisan pengeluaran — peribadi, di premis, tanpa yuran AI setiap pengguna.' },
  { t: 'Runcit & laman hadapan', b: 'Amaran orang ramai dan pencerobohan, serta pengecaman plat nombor untuk pemantauan laman hadapan dan pandu lalu.' },
  { t: 'Gudang & logistik', b: 'Pantau limbungan muatan dan halaman — pengesanan kenderaan dan orang dengan lintasan garis di pintu, semuanya pada perkakasan edge.' },
  { t: 'Hartanah & perimeter', b: 'Pengesanan pencerobohan selepas waktu kerja dengan rakaman selamat tersulit yang kekal di tapak.' },
  { t: 'Fasiliti berkawalan akses', b: 'Pembaca lencana, jadual akses dan sekatan tapak diputuskan di tapak, dengan kamera yang memerhati pintu dan penderia yang merasainya terbuka melaporkan ke garis masa yang sama.' },
  { t: 'Pertanian & tapak terpencil', b: 'Kesan haiwan atau penceroboh merentas tanah dengan sambungan lemah atau tiada; pengesanan dan rakaman berjalan sepenuhnya secara tempatan.' },
  { t: 'Rumah jagaan & klinik', b: 'Amaran pergerakan selepas waktu kerja dan gaya-terjatuh dengan rakaman yang tidak pernah meninggalkan bangunan — privasi secara lalai.' },
  { t: 'Perindustrian & utiliti', b: 'Peraturan zon dan lintasan garis untuk kawasan larangan dan peralatan, pada perkakasan lasak di tempat sambungan tidak boleh dipercayai.' },
  { t: 'Armada berbilang tapak', b: 'Satah kawalan mengambil alih banyak nod edge melalui LAN — kamera, penderia dan pengawal pintu sekali — memetakan setiap tapak dan bangunan, dan menyampaikan paparan langsung kembali kepada operator, dengan pelan lantai setiap bangunan yang meletakkan setiap kamera dan liputannya.' },
];

const A = [
  { s: 'Perdana · dalam pembangunan aktif', b: 'Nod kamera & risikan video edge berdiri sendiri: ONVIF, pengesanan AI, NVR, paparan langsung WebRTC, penyulitan, dan pemadanan LAN.' },
  { s: 'Hab penderia · tersedia sekarang', b: 'NVR, tetapi untuk penderia: sesentuh pintu, gerakan, suhu, asap, kebocoran, meter kuasa dan penyongsang Modbus/SunSpec melalui broker MQTT terbenam — dengan peraturan, amaran, adegan dan jadual, kanvas aliran visual, penggerakan selamat, dan sejarah telemetri.' },
  { s: 'Kawalan akses · dalam pembangunan', b: 'Pengawal pintu: pembaca lencana OSDP melalui RS-485 atau IP, dengan orang, kumpulan, jadual, cuti, sekatan tapak dan PIN duress semuanya diputuskan pada perkakas itu sendiri, jadi sambungan terputus tidak pernah bermakna pintu yang tidak mahu terbuka. Boleh diambil alih ke dalam armada seperti mana-mana nod lain — belum tersedia untuk dimuat turun.' },
  { s: 'Satah kawalan · tersedia sekarang', b: 'Satah kawalan armada yang menemui, mengambil alih, dan mengurus nod kamera, penderia dan pintu sekali — memetakan setiap tapak, bangunan, dan nod dengan pelan lantai setiap bangunan dan liputan kamera — dan mengaitkan peristiwa merentasinya: gerakan pada kamera DAN pintu terbuka DAN tiada lencana diterima. Ia juga menulis ringkasan armada harian, menjawab soalan tentang armada, dan menghasilkan laporan PDF boleh cetak.' },
  { s: 'Identiti · tersedia sekarang', b: 'Pintu hadapan log masuk tunggal (SSO): satu identiti bersekutu merentas setiap aplikasi, dengan akaun tempatan, LDAP / Active Directory perusahaan, SSO desktop Kerberos SPNEGO, dan pembekal OIDC generik — serta pengesahan berbilang faktor TOTP dan WebAuthn, pemetaan kumpulan-ke-peranan, kawalan akses setiap aplikasi, dan jejak audit yang tidak boleh diubah. Satu binari Go tulen yang berjalan sepenuhnya di intranet anda, tiada egress.' },
];

const SHOT_ALT = [
  'Grid berbilang kamera langsung dalam susun atur 3×2, dengan lencana AI “Kehadiran dikesan (orang)” pada bingkai dua suapan',
  'Tab Pengesanan AI: peraturan kehadiran dengan zon pengesanan enam titik dilukis pada bingkai kamera sebenar dalam editor zon',
  'Ajar — bestari berpandu tanpa jargon untuk mengajar kamera kemahiran baharu (Namakan · Jenis apa · Di mana · Tunjuk contoh · Semak ketepatan · Hidupkannya), pada langkah “Kemahiran jenis apa ini?” dengan tiga pilihan: kenali objek baharu, bezakan baik daripada rosak, atau kesan apa-apa yang luar biasa',
  'Halaman Rakaman: garis masa NVR berterusan sehari dengan penanda klip peristiwa, dan senarai klip dengan main, muat turun, dan padam',
  'Carian Objek: carian merentas kamera pada garis masa tentang apa yang dilihat setiap kamera — ditapis mengikut kamera, julat tarikh, jenis objek dan keyakinan — di mana setiap hasil membawa lakaran kecil rakaman dengan objek dikesan dikotakkan, serta tindakan main dan besarkan',
  'Papan pemuka analitik peristiwa: KPI jumlah, belum dibaca, kritikal dan amaran, carta bar peristiwa mengikut masa, dan donat kategori serta keterukan',
  'Analitik papan pemuka: peta haba aktiviti pengesanan mengikut jam dalam sehari dan hari dalam seminggu, mendedahkan bila setiap kamera paling sibuk',
  'Tetapan Sandaran & Pemulihan: sandaran konfigurasi dilindungi frasa laluan bagi kamera, pengesanan AI, pemberitahuan dan tetapan aplikasi, dengan pemulihan',
  'Tetapan Versi & Kesihatan dipapar dalam bahasa Arab (kanan ke kiri): versi aplikasi dan teras, semakan kemas kini, dan jubin kesihatan perkhidmatan DB, kamera dan cache',
];

const NAV = ['Ciri', 'Cara ia berfungsi', 'Perkakasan', 'Pameran', 'Kegunaan', 'Aplikasi', 'Harga', 'Muat turun'];

export default {
  ...en,
  meta: {
    title: 'r450k — Suite keselamatan & automasi di premis yang peribadi',
    description:
      'Kamera AI, penderia dan kawalan akses pintu di premis dengan satu satah kawalan armada dan log masuk tunggal: penemuan ONVIF, pengesanan YOLO, rakaman NVR, paparan langsung WebRTC, pembaca lencana OSDP, telemetri MQTT, dan penyulitan semasa rehat — berjalan pada perkakasan sekecil Raspberry Pi.',
  },
  brand: { ...en.brand, tagline: 'Kecerdasan keselamatan di premis yang peribadi' },
  nav: en.nav.map((n, i) => ({ ...n, label: NAV[i] })),
  navCta: 'Terokai aplikasi',
  hero: {
    ...en.hero,
    eyebrow: 'Di premis · AI Edge · Tanpa awan',
    titleLead: 'Kecerdasan keselamatan yang berjalan',
    rotate: ['di edge.', 'pada rangkaian anda.', 'pada Raspberry Pi.', 'tanpa awan.'],
    subtitle:
      'r450k ialah suite aplikasi yang dipasang pada perkakasan yang anda sudah miliki: kamera AI, penderia bangunan dan pembaca lencana di edge, satu satah kawalan yang mengambil alih kesemuanya dan mengaitkan peristiwa merentasinya, serta log masuk tunggal yang menyatukannya. Ia berskala daripada satu Raspberry Pi yang memantau beberapa kamera kepada pelayan GPU yang menjalankan AI masa nyata merentas berpuluh-puluh. Rakaman anda — dan segala yang lain — tidak pernah meninggalkan rangkaian anda.',
    primaryCta: { ...en.hero.primaryCta, label: 'Lihat ciri' },
    secondaryCta: { ...en.hero.secondaryCta, label: 'Cara ia berfungsi' },
    stats: en.hero.stats.map((s, i) => ({ ...s, label: ['Kamera, penderia, pintu, identiti, armada', 'Di premis, peribadi secara lalai', 'Bermula pada Pi 4 — berskala ke pelayan GPU'][i] })),
    note: 'Peringkat permulaan berjalan pada Raspberry Pi 4 (beberapa kamera dengan AI gerakan atau model-nano). Pengesanan masa nyata berbilang kamera dan mengajar kamera kemahiran anda sendiri berskala ke mini-PC dan GPU NVIDIA. Hab penderia, pengawal pintu, broker identiti dan satah kawalan ialah binari Go tulen tunggal dan memerlukan jauh lebih sedikit.',
  },
  features: {
    ...en.features,
    kicker: 'Keupayaan',
    title: 'Segala yang diperlukan oleh harta di premis yang peribadi.',
    lead: 'Kamera, penderia, pintu, identiti dan pengurusan armada — dibina untuk berjalan pada peranti kecil, sepenuhnya pada rangkaian anda sendiri.',
    items: en.features.items.map((it, i) => ({ ...it, title: F[i].t, body: F[i].b })),
  },
  how: {
    ...en.how,
    kicker: 'Cara ia berfungsi',
    title: 'Daripada peranti kosong kepada amaran boleh tindak dalam empat langkah.',
    steps: en.how.steps.map((s, i) => ({ ...s, title: H[i].t, body: H[i].b })),
  },
  tiers: {
    ...en.tiers,
    kicker: 'Perkakasan',
    title: 'Berskala dengan perkakasan anda.',
    lead: 'Nod kamera ialah yang paling berat: ia berjalan daripada peranti sebesar poket kepada pelayan GPU, dan anda pilih peringkatnya. Hab penderia, pengawal pintu, broker identiti dan satah kawalan ialah binari Go tulen tunggal yang muat selesa di sisinya. (Bilangan kamera adalah anggaran; penganggar kapasiti terbina mengukur angka sebenar untuk hos anda.)',
    recommended: 'Disyorkan',
    items: en.tiers.items.map((it, i) => ({ ...it, name: T[i].n, load: T[i].l, detail: T[i].d })),
  },
  showcase: {
    ...en.showcase,
    kicker: 'Lihat ia beraksi',
    title: 'Konsol dibina untuk operator, bukan sekadar pentadbir.',
    lead: 'Skrin ini ialah MyMataSan, nod kamera: grid berbilang kamera langsung, peraturan pengesanan dilukis pada bingkai sebenar, mengajar kamera secara berpandu tanpa jargon ML, rakaman NVR berterusan, analitik peristiwa, sandaran disulitkan, dan versi & kesihatan. Setiap aplikasi dalam suite berkongsi cangkerang yang sama — dalam pelayar, dalam empat bahasa.',
    tabs: { live: 'Paparan langsung', detection: 'Pengesanan AI', teach: 'Ajar', recordings: 'Rakaman', objectsearch: 'Carian objek', dashboard: 'Papan pemuka', heatmap: 'Peta haba aktiviti', backup: 'Sandaran & pemulihan', health: 'Versi & kesihatan' },
    shots: en.showcase.shots.map((s, i) => ({ ...s, alt: SHOT_ALT[i] })),
  },
  useCases: {
    ...en.useCases,
    kicker: 'Kegunaan',
    title: 'Dibina untuk tempat yang tidak boleh menghantar datanya ke awan.',
    items: en.useCases.items.map((it, i) => ({ ...it, title: U[i].t, body: U[i].b })),
  },
  apps: {
    ...en.apps,
    kicker: 'Platform',
    title: 'Satu platform, lima aplikasi.',
    subtitle: 'r450k ialah platform bermodul. mymatasan ialah nod kamera edge, myiotsan hab penderia edge, dan mypintusan pengawal pintu; satah kawalan mengambil alih ketiga-tiganya dan mengaitkan peristiwa merentasinya, dan identiti dikongsi menyatukannya.',
    available: 'Tersedia',
    platform: 'Platform',
    items: en.apps.items.map((it, i) => ({ ...it, status: A[i].s, body: A[i].b })),
  },
  downloads: {
    ...en.downloads,
    kicker: 'Dapatkan aplikasi',
    title: 'Muat turun & host sendiri.',
    subtitle: 'Jalankan satu nod di satu lokasi, atau seluruh armada dari satu control plane. Setiap satu ialah satu pemasangan sahaja, lengkap dengan UI web dan konfigurasi lalai. (mypintusan, pengawal pintu, masih dalam pembangunan dan belum boleh dimuat turun.)',
    products: {
      mymatasan: {
        name: 'MyMataSan',
        tagline: 'Nod edge — satu pemasangan bagi setiap lokasi. Kamera ONVIF, pengesanan AI atas peranti, rakaman NVR dan paparan langsung WebRTC. Disertakan UI web, skrip pekerja AI dan konfigurasi lalai.',
        dockerHint: 'Jalankan imej berbilang-seni bina (ffmpeg disertakan):',
      },
      myseliasan: {
        name: 'MySeliaSan',
        tagline: 'Control plane armada — temui, ambil alih dan urus banyak nod MyMataSan dan MyIotSan dari satu tempat, lihat kamera dan penderia mereka tanpa sekali pun mendedahkan nod, dan kaitkan peristiwa merentas kedua-duanya. Satu binari Go tulen: tiada ffmpeg, tiada Python.',
        dockerHint: 'Jalankan imej berbilang-seni bina:',
      },
      myiotsan: {
        name: 'MyIotSan',
        tagline: 'Hab penderia — satu pemasangan bagi setiap lokasi. Penderia bangunan & keselamatan melalui broker MQTT terbenam, dengan stor telemetri bertapis-hingar, peraturan ambang dan korelasi, amaran, dan penggerakan selamat sahkan-untuk-laksana. Satu binari Go tulen: tiada ffmpeg, tiada Python. Membuka port MQTT (1883) untuk peranti.',
        dockerHint: 'Jalankan imej berbilang-seni bina (membuka port MQTT 1883 untuk peranti):',
      },
      myidsan: {
        name: 'MyIDSan',
        tagline: 'Broker identiti — satu log masuk merentas setiap aplikasi. Akaun tempatan serta LDAP / Active Directory perusahaan, SSO desktop Kerberos SPNEGO dan OIDC generik, dengan pemetaan kumpulan-ke-peranan dan RBAC setiap aplikasi. Satu binari Go tulen yang berjalan sepenuhnya di intranet anda: tiada ffmpeg, tiada Python, tiada egress.',
        dockerHint: 'Jalankan imej berbilang-seni bina:',
      },
    },
    license: 'Percuma untuk kegunaan peribadi dan bukan komersial — individu, badan bukan untung, pendidikan, dan penyelidikan. Kegunaan komersial atau dalam perniagaan, dan sebarang penjualan semula, memerlukan lesen komersial.',
    latest: 'Keluaran terkini',
    dockerHint: 'Jalankan imej berbilang-seni bina (ffmpeg disertakan):',
    loading: 'Memuatkan keluaran terkini…',
    unavailable: 'Muat turun sedang disediakan — semak semula sebentar lagi.',
    formTitle: 'Muat turun anda telah bermula 🎉',
    formBlurb: 'Satu soalan ringkas sebelum anda pergi — bagaimana anda mengetahui tentang kami? Ambil 10 saat sahaja dan banyak membantu.',
    formClose: 'Tutup',
  },
  pricing: {
    ...en.pricing,
    kicker: 'Harga',
    title: 'Percuma untuk anda. Adil untuk perniagaan.',
    lead: 'Kegunaan peribadi dan bukan komersial adalah percuma — selamanya. Perniagaan mengekalkan projek ini hidup dengan lesen komersial yang mudah. Ini harga awal; kita akan selaraskan bersama apabila ia berkembang.',
    popular: 'Paling popular',
    note: 'Harga dalam USD. Kegunaan bukan komersial kekal percuma. Tidak pasti yang mana sesuai? Hubungi kami dan kita fikirkan bersama.',
    contactTitle: 'Hubungi kami',
    contactBlurb: 'Beritahu kami tentang tapak anda dan apa yang anda perlukan — kami akan hubungi anda semula.',
    contactClose: 'Tutup',
    tiers: en.pricing.tiers.map((t, i) => ({
      ...t,
      name: ['Peribadi', 'Perniagaan', 'Armada & Perusahaan'][i],
      period: ['bukan komersial', '/ tapak · tahun', 'berbilang tapak'][i],
      cta: ['Muat turun', 'Dapatkan lesen', 'Hubungi kami'][i],
      features: [
        ['Setiap aplikasi, setiap ciri, peranti tanpa had', 'AI atas peranti, NVR, penyulitan, sandaran', 'Peribadi, hobi, bukan untung & pendidikan', 'Sokongan komuniti'],
        ['Lesen komersial untuk satu pelayan / tapak', 'Kamera dan peranti tanpa had pada pelayan itu', 'Semua kemas kini untuk tempoh berlesen', 'Sokongan e-mel keutamaan'],
        ['Banyak tapak melalui satah kawalan', 'Latihan & integrasi model AI tersuai', 'Onboarding & sokongan keutamaan', 'Pelesenan volum & OEM'],
      ][i],
    })),
  },
  finalCta: {
    ...en.finalCta,
    title: 'Simpan video anda — dan risikan anda — pada rangkaian anda sendiri.',
    body: 'r450k membawa AI kamera bertaraf awan, automasi penderia dan kawalan akses ke perkakasan yang anda sudah miliki, dengan privasi sebagai lalai dan bukan naik taraf.',
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
    support: 'Belanja saya kopi',
    supportNudge: 'Walau tip kecil pun membantu 💛',
    supportBlurb: 'Jika r450k menjimatkan langganan awan anda, secawan kopi memastikan ia berterusan.',
  },
  footer: {
    ...en.footer,
    tagline: 'r450k · Kecerdasan keselamatan di premis yang peribadi',
    note: 'Berjalan di premis. Data anda tidak pernah meninggalkan rangkaian anda.',
    rights: 'Hak cipta terpelihara.',
    columns: en.footer.columns.map((c) => ({ ...c, heading: 'Produk', links: c.links.map((l, j) => ({ ...l, label: ['Ciri', 'Cara ia berfungsi', 'Kegunaan', 'Harga', 'Muat turun'][j] })) })),
  },
};
