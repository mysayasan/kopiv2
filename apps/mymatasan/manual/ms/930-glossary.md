---
title: Glosari
category: appendix
categoryLabel: Lampiran
summary: Perkataan yang digunakan produk ini, dan maksud setiap satu di sini.
order: 930
---

# Glosari

Istilah sebagaimana produk ini menggunakannya. Sesetengahnya digunakan secara longgar di tempat lain;
inilah maksud yang terpakai pada skrin-skrin ini.

**Ambang** — keyakinan minimum yang boleh diterima sesuatu peraturan.

**Amaran** — apa yang dihasilkan peraturan apabila ia dicetuskan: cap masa, kamera, apa yang dilihat,
gambar petikan, dan pautan kepada rakaman apabila ada.

**Bingkai minimum** — berapa banyak bingkai berturut-turut mesti mengandungi objek sebelum peraturan
dicetuskan. Kawalan terbaik terhadap kelipan.

**Cap wajah** — perwakilan matematik bagi wajah orang yang didaftarkan. Data biometrik, dengan
tanggungjawab undang-undang yang melekat. Lihat [Orang](people#consent).

**Carian rupa** — menyusun penampakan yang direkodkan mengikut sejauh mana ia kelihatan serupa
dengan yang anda pilih, bukan mengikut kelas objek. Senarai pendek untuk disahkan dengan mata,
bukan pengecaman identiti. Lihat [Cari yang serupa](object-search#appearance).

**Gambar petikan** — imej pegun yang ditangkap pada detik peraturan dicetuskan, dengan pengesanan
berkotak dan berlabel.

**Kadar bit** — berapa banyak data dihasilkan sesuatu strim setiap saat. Pemacu utama bagi berapa
banyak cakera yang dimakan sesuatu tempoh pengekalan.

**Kelas** (kelas objek) — nama yang anda takrifkan yang memetakan kepada satu atau lebih label model.
*Kenderaan* = `car`, `truck`, `bus`. Inilah yang ditulis peraturan terhadapnya. Lihat
[Kelas objek](object-classes).

**Kemahiran** — sesuatu yang telah diajar kepada kamera untuk dikenali melalui
[Mod mengajar](teach-mode).

**Keyakinan** — sejauh mana pastinya model tentang sesuatu pengesanan, 0 hingga 1. Ambang sesuatu
peraturan ialah minimum yang diterimanya.

**Klip peristiwa** — rakaman yang diekstrak di sekeliling pencetus, termasuk pra-gulung dan
pasca-gulung.

**Kunci armada** — rahsia dikongsi yang menjadikan nod boleh ditemui dan boleh diambil oleh satu
satah kawalan myseliasan. Lihat [Menyambung ke satah kawalan](control-plane#fleet-key).

**Label** — perkataan tepat yang dikeluarkan model bagi sesuatu yang dikenalinya: `person`, `car`,
`fire hydrant`. Dipadankan dengan tepat; dikumpulkan menjadi kelas.

**Lintasan garis** — mod peraturan yang dicetuskan apabila sesuatu melintasi garis yang dilukis,
boleh dihadkan kepada satu arah sahaja.

**LPR** — pengecaman plat nombor. Membaca teks plat, dan apabila tersedia jenis dan warna kenderaan.
Lihat [Api, asap dan plat](fire-smoke-and-plates#lpr).

**MJPEG** — mod penghantaran sandaran di mana peranti menukar video menjadi imej pegun untuk pelayar.
Berfungsi di mana-mana, memakan jauh lebih banyak CPU berbanding WebRTC.

**Model stok** — model pengesanan asas yang sentiasa hidup yang mengenali kelas harian umum. Berjalan
bersama mana-mana model tersuai. Lihat [Model](how-detection-works#models).

**Nod** — peranti yang diuruskan oleh satah kawalan myseliasan.

**ONVIF** — standard industri untuk menemui dan menguruskan kamera IP. Pilihan: kamera RTSP sahaja
turut berfungsi.

**Operator** — peranan pertengahan: menyemak rakaman, mengakui, PTZ, cakap-balas. Tidak boleh memadam.
Lihat [Pengguna dan peranan](users-and-roles#roles).

**Pemadaman kripto** — memusnahkan kunci penyulitan supaya teks sifer tidak boleh dibaca lagi, dan
bukan menulis ganti data. Inilah yang menjadikan set semula kilang terjamin pada SSD.

**Pemerhati** — peranan paling terhad: video langsung dan hakikat bahawa amaran berlaku, tidak lebih.

**Penampakan** — satu entri dalam garis masa objek: satu objek dilihat, dikumpulkan merentasi bingkai
dan bukan direkod setiap bingkai. Lihat [Carian objek](object-search#sightings).

**Pengekalan** — berapa hari rakaman disimpan sebelum pembersihan automatik. Ditetapkan bagi setiap
kamera. Lihat [Konfigurasi rakaman](recording-configuration#retention).

**Peraturan** — arahan tetap pada satu kamera: mod, kelas, zon, jadual, ambang dan penghalaan. Lihat
[Mencipta peraturan pengesanan](detection-rules).

**Peristiwa diagnostik** — sampel yang direkodkan untuk menunjukkan apa yang dilihat pengesan,
ditanda supaya ia boleh dibersihkan berasingan daripada pengesanan sebenar.

**Pra-gulung** — saat rakaman yang disimpan dari *sebelum* pencetus dalam klip peristiwa. Biasanya
bahagian yang anda sebenarnya perlukan.

**PTZ** — pan, tilt dan zum.

**RTSP** — protokol yang digunakan kamera untuk menstrim video.

**Segmen** — bahagian rakaman berterusan dengan panjang tetap. Segmen lebih pendek kehilangan lebih
sedikit apabila ranap.

**Set semula kilang** — mencarik semua data, memusnahkan kunci, membina semula pangkalan data dan
memulakan semula ke persediaan larian pertama. Lihat
[Padam selamat dan set semula kilang](secure-wipe-and-reset).

**Strim pengesanan** — strim kamera yang dibaca AI. Biasanya sub-strim, dan bebas daripada strim
rakaman. Lihat [tab Strim](camera-properties#stream).

**Strim utama** — strim beresolusi penuh kamera. Yang anda rakam.

**Sub-strim** — strim sekunder beresolusi lebih rendah kamera. Yang sepatutnya digunakan paparan
langsung dan pengesanan.

**Tempoh sejuk** — berapa lama peraturan kekal senyap selepas dicetuskan, supaya satu peristiwa
berterusan menghasilkan satu amaran.

**WebRTC** — laluan paparan langsung yang cekap, di mana pelayar menyahkod strim kamera secara terus.
Ditunjukkan sebagai **Langsung** pada jubin.

**Zon** — kawasan bingkai yang dilukis yang dipedulikan sesuatu peraturan. Kawasan pada *bingkai*,
bukan di dunia — menggerakkan kamera menggerakkan zon. Lihat [Zon](detection-rules#zones).
