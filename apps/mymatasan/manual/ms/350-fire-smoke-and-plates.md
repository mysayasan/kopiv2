---
title: Api, asap dan plat nombor
category: detection
categoryLabel: Pengesanan & AI
summary: Dua pengesanan khusus — apa yang diperlukan setiap satu, dan di mana setiap satu gagal.
order: 350
---

# Api, asap dan plat nombor

Dua pengesanan berkelakuan cukup berbeza daripada pengesanan objek biasa sehingga wajar mendapat
halamannya sendiri.

## Api dan asap {#fire}

Api dan asap ialah kelas objek biasa dari sudut peraturan — anda mengesannya seperti anda mengesan
seseorang. Yang berbeza ialah **model stok tidak mengenalinya**. Anda memerlukan model yang dilatih
untuk api dan asap, diimport dan diaktifkan dalam **Tetapan → AI**.

Anda mendapatkan model itu sendiri. Apabila satu diimport, labelnya dikesan secara automatik dan
dipetakan kepada kategori bahaya *Api* dan *Asap*, termasuk variasi penamaan biasa yang digunakan
model berbeza, jadi anda jarang perlu menyambungkan apa-apa secara manual.

### Menjadikannya berguna, bukan menjengkelkan {#fire-tuning}

Pengesanan api dan asap mempunyai profil positif palsu yang tersendiri, dan berbaloi merancang
untuknya:

- **Matahari terbenam, lampu depan, lampu brek, kerja kimpalan, pakaian hi-vis** dibaca sebagai api.
- **Wap, ekzos, habuk, kabus dan awan rendah** dibaca sebagai asap.

Yang bermakna:

- **Zon dengan ketat.** Kecualikan langit, jalan dan apa-apa yang mempunyai lampu.
- **Naikkan bingkai minimum.** Api sebenar berterusan; lampu depan yang lalu tidak.
- **Gunakan jadual di mana sumber palsu terikat masa** — kamera menghadap barat pada waktu matahari
  terbenam ialah masalah yang boleh diramal pada jam yang boleh diramal.

> [!WARNING]
> Ini bukan penggera kebakaran. Ia kamera yang perasan sesuatu yang kelihatan seperti api. Ia tidak
> menggantikan pengesan asap, pemercik air, atau mana-mana sistem keselamatan nyawa yang anda
> dikehendaki memilikinya, dan ia tidak boleh sekali-kali menjadi satu-satunya perkara antara
> kebakaran dan orang di dalam bangunan.

## Pengecaman plat nombor {#lpr}

LPR membaca plat kenderaan dalam sesuatu zon dan, apabila tersedia, melaporkan jenis dan warna
kenderaan bersama teks plat.

### Apa yang diperlukannya {#lpr-requirements}

- **Model plat**, ditetapkan dalam **Tetapan → AI → License Plate Model**.
- **Resolusi.** Plat tidak boleh dibaca pada strim beresolusi rendah. LPR secara automatik menggunakan
  strim tertinggi kamera, dan mod itu **disembunyikan sepenuhnya** pada kamera yang resolusinya
  terlalu rendah untuk berbaloi dicuba — jika anda tidak menjumpai LPR pada sesuatu kamera, itulah
  sebabnya.

Resolusi pada plat itulah yang penting, bukan megapiksel utama kamera. Kamera 4K yang meliputi
keseluruhan halaman hadapan mungkin meletakkan lebih sedikit piksel pada plat berbanding kamera 1080p
yang dihalakan ke satu lorong. Jika plat penting, khaskan satu kamera untuk lorong itu.

### Bila hendak memberi amaran {#lpr-modes}

Tiga mod, dan memilih yang betul ialah kebanyakan nilainya:

| Mod | Dicetuskan pada | Guna untuk |
|---|---|---|
| **Mana-mana plat yang boleh dibaca** | Setiap plat yang boleh dibacanya | Merekod trafik melalui satu titik |
| **Hanya plat dalam senarai pantau** | Plat yang anda senaraikan | Ketibaan VIP atau armada — "beritahu saya bila kereta pengarah sampai" |
| **Mana-mana plat BUKAN dalam senarai** | Apa-apa yang tidak disenaraikan | Kenderaan tidak dikenali di pintu kakitangan — kes keselamatan |

Yang ketiga biasanya yang orang benar-benar mahukan, dan ia yang mereka konfigurasikan paling akhir.

### Senarai plat {#lpr-list}

Satu plat setiap baris, atau dipisahkan koma. Ruang dan sengkang diabaikan, dan pemadanan bertoleransi
terhadap satu ralat aksara — kerana OCR mengelirukan `0`/`O` dan `8`/`B` setiap hari, dan pemadanan
tepat akan terlepas separuh ketibaan kereta yang jelas ada dalam senarai.

Toleransi itu berkesan dua hala: pada senarai pengecualian yang besar, plat yang berbeza satu aksara
daripada plat yang tersenarai akan dianggap tersenarai. Kekalkan senarai pantau sependek yang
dibenarkan tugas.

### Keyakinan OCR {#lpr-confidence}

**Keyakinan OCR minimum** berasingan daripada ambang pengesanan. Pengesanan memutuskan ada plat; OCR
memutuskan apa yang tertulis padanya.

Naikkannya dan anda mendapat bacaan yang lebih sedikit tetapi lebih boleh dipercayai. Turunkannya dan
anda mendapat lebih banyak bacaan, termasuk yang bercelaru — yang lebih penting daripada biasa dalam
mod *pengecualian*, di mana plat yang tersalah baca ialah amaran tentang kereta yang sebenarnya ada
dalam senarai anda sepanjang masa.

### Jangkaan yang realistik {#lpr-limits}

Sudut, kelajuan, cuaca, kotoran, silau lampu dan plat bukan standard semuanya memakan ketepatan.
Kamera yang diletakkan dengan baik pada lorong perlahan membaca kebanyakan plat kebanyakan masa;
kamera yang bersudut merentasi jalan laju tidak, dan tiada tetapan yang membetulkan geometri.
