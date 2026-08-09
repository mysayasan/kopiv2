---
title: Penyelesaian masalah
category: appendix
categoryLabel: Lampiran
summary: Gejala yang benar-benar dihadapi orang, dan urutan untuk menyemak sesuatu.
order: 910
---

# Penyelesaian masalah

Gejala mengikut urutan lazimnya dilaporkan. Setiap senarai disusun supaya semakan termurah datang
dahulu.

## Saya tidak boleh log masuk {#sign-in}

- **"Anda tiada kebenaran untuk tindakan ini" sejurus selepas log masuk** — peranan akaun itu tidak
  membenarkan sesuatu yang diperlukan aplikasi semasa permulaan. Minta pentadbir menyemak peranan
  itu, atau log masuk dengan akaun pentadbir untuk mengesahkan kelayakan itu sendiri baik.
- **Kiraan detik ditunjukkan** — alamat itu dikunci selepas kegagalan berulang. Tunggu; tiada sesiapa
  boleh memendekkannya. Lihat [Apabila log masuk dikunci](first-sign-in#lockout).
- **Ia meminta fail kunci pemulihan** — penyulitan dihidupkan dan kunci induk tidak boleh dibaca.
  Lihat [skrin pemulihan](encryption-at-rest#recovery-gate).
- **Saya telah kehilangan kata laluan pentadbir** — ia ditetapkan semula pada konsol pada mesin itu
  sendiri, bukan daripada pelayar. Lihat
  [Log masuk buat kali pertama](first-sign-in#first-password).

## Kamera luar talian {#camera-offline}

Urutan penuh dalam [Kesihatan kamera](camera-health#troubleshooting). Versi ringkas: adakah hanya
yang ini → bolehkah anda membuka halaman web kamera itu daripada rangkaian peranti → adakah kelayakan
berubah → adakah alamatnya berubah → adakah profil strimnya masih sah.

## Paparan langsung hitam, tersekat-sekat atau perlahan {#live-view}

1. **Baca status jubin.** "Sandaran MJPEG" bermakna pelayar tidak boleh memainkan kodek kamera secara
   terus, dan peranti sedang menukarkannya — mahal bagi setiap jubin. Lihat
   [status jubin](live-views#tile-status).
2. **"WebRTC perlukan H264"** — tukar strim kamera kepada H.264 dan laluan cekap akan kembali.
3. **Paparan langsung dihalakan ke strim utama.** Tetapkan sub-strim pada
   [tab Strim](camera-properties#stream).
4. **Jubin hitam, kamera dalam talian** — biasanya kelayakan yang mengesahkan untuk pengurusan tetapi
   tidak untuk penstriman, atau profil strim yang tidak lagi wujud. **Cari Strim**, kemudian **Uji
   RTSP**.
5. **Semuanya tersekat-sekat serentak** — mesin itu, bukan kamera. Lihat
   [Memerhati mesin itu sendiri](machine-health#cpu).

## Saya tidak dapat amaran {#no-alerts}

Mengikut urutan: adakah ada **peraturan**; adakah ia **didayakan** dan dalam **jadual**; adakah
kamera **dalam talian**; adakah **runtime AI dan model** hadir; dan barulah, adakah **penghantaran**
dikonfigurasikan. Butiran dalam [Pemberitahuan](notifications#not-arriving).

Jawapan tunggal paling biasa ialah yang pertama: rakaman dihidupkan, dan tiada peraturan pernah
dicipta. Rakaman menghasilkan rakaman video, bukan amaran.

## Saya dapat terlalu banyak amaran {#too-many}

Hampir selalu geometri dan bukan kepekaan:

1. **Lukis zon.** Kamera yang memerhati pintu pagar biasanya turut memerhati laluan pejalan kaki
   awam. Lihat [zon](detection-rules#zones).
2. **Naikkan bingkai minimum** kepada 2 atau 3. Membunuh kelipan, bayang dan serangga.
3. **Kemudian** naikkan ambang keyakinan.
4. **Tambah jadual**, atau jadual *jeda semasa* bagi tempoh sibuk.
5. **Sempitkan kelas** — "apa sahaja" sepadan dengan segalanya.

Semak [kamera paling bising](dashboard#noise) untuk mencari peraturan mana yang sebenarnya
bertanggungjawab; ia jarang yang disalahkan orang.

## Pengesanan terlepas sesuatu {#misses}

- **Ambang keyakinan terlalu tinggi** bagi keadaan kamera itu.
- **Zon tidak meliputi** tempat ia sebenarnya berlaku. Zon ialah kawasan pada bingkai — jika kamera
  digerakkan, zon bergerak bersamanya.
- **Kelas tiada dalam peraturan**, atau kategorinya tiada label yang sepadan, atau modelnya tidak
  aktif. Kategori yang ditanda **model tidak aktif** tidak mengesan apa-apa.
- **Kamera tidak dapat melihatnya.** Gelap, cahaya belakang, basah, terlalu jauh, tidak fokus. Tiada
  model memulihkan butiran yang tidak pernah dirakam.

## "Tiada klip dirakam" pada amaran {#no-clip}

Sama ada rakaman dimatikan untuk kamera itu ketika itu, atau rakaman telah melepasi pengekalannya dan
dibersihkan. Kedua-duanya bukan kerosakan — lihat [membaca pengesanan](notifications#reading).

## Cakera semakin penuh {#disk}

Gunakan **Bersihkan yang tamat tempoh** dahulu — ia hanya membuang rakaman yang sudah melepasi
pengekalan. Kemudian pilih antara memendekkan pengekalan, menurunkan kadar bit, atau menambah storan.
Lihat [Storan dan kapasiti](storage-and-capacity#disk).

Jangan sekali-kali halakan laluan storan ke pemacu sistem.

## Cakap-balas tidak berfungsi {#talk-back}

Kebanyakannya kata laluan yang salah: kamera TP-Link Tapo mahukan kata laluan **akaun awan TP-Link**
anda, bukan kata laluan strim kamera. Tab Akses kamera mempunyai senarai semak penuh. Lihat
[Cakap-balas](live-views#talk-back).

## Plat nombor tidak dibaca {#lpr}

- **Mod LPR tidak ditawarkan pada kamera ini** — resolusinya terlalu rendah, dan pilihan itu
  disembunyikan dan bukan dibiarkan mengecewakan. Lihat
  [keperluan LPR](fire-smoke-and-plates#lpr-requirements).
- **Tiada model plat** ditetapkan dalam Tetapan → AI, atau **kebergantungan OCR** hilang — semak
  Versi & Kesihatan.
- **Bacaan bercelaru** — geometri. Sudut, kelajuan dan silau mendominasi; kamera yang dikhaskan untuk
  satu lorong perlahan membaca plat yang kamera halaman hadapan yang luas tidak akan sekali-kali.

## Selepas kemas kini sesuatu berkelakuan berbeza {#after-update}

Semak versi dan kebergantungan runtime pada
[Versi & Kesihatan](updates-and-restart#dependencies), kemudian mulakan semula sekali. Jika sesuatu
kamera berubah kelakuan, jalankan semula **Cari Strim** — kemas kini perisian tegar menomborkan semula
profil lebih kerap daripada yang anda jangkakan.

## Tiada di sini yang sepadan {#escalating}

Kumpulkan, sebelum bertanya:

- **Versi tepat** — aplikasi, teras dan commit, daripada Versi & Kesihatan.
- **Apa yang berubah** sejurus sebelum ia bermula.
- Sama ada ia menjejaskan **satu kamera atau semuanya** — perbezaan tunggal itu menghapuskan
  kebanyakan kemungkinan.
- **Suapan pemberitahuan** sekitar masa ia bermula; entri kesihatan kamera dan kesihatan mesin kerap
  menjelaskan apa yang kelihatan seperti kerosakan aplikasi.
