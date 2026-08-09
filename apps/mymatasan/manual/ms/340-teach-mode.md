---
title: Mengajar kamera kemahiran baharu
category: detection
categoryLabel: Pengesanan & AI
summary: Ajar kamera mengenali sesuatu yang tidak diketahui model stok — tanpa pengetahuan AI.
order: 340
---

# Mengajar kamera kemahiran baharu

Model stok tahu perkara umum: orang, kenderaan, haiwan. Ia tidak tahu perkara *anda* — uniform kurier
anda, forklift anda, perbezaan antara bahagian yang baik dan yang cacat pada barisan anda.

Mod mengajar ialah cara anda menunjukkan perkara itu kepada kamera, tanpa mengetahui apa-apa tentang
pembelajaran mesin.

> [!NOTE]
> Mod mengajar dihantar berperingkat. Menamakan kemahiran, memilih jenisnya, memilih kamera dan
> kawasan, serta menjalankan sesi mengajar untuk mengumpul contoh semuanya berfungsi hari ini.
> Langkah **semakan ketepatan** dan **hidupkannya** akan tiba dalam kemas kini kemudian, dan bestari
> menyatakannya pada langkah tersebut. Anda boleh menyediakan kemahiran dan mengumpul contoh
> sekarang; mengaktifkan kemahiran yang diajar datang bersama kemas kini itu.

## Sebelum anda bermula {#prerequisites}

Mengajar memerlukan runtime AI dipasang — yang sama digunakan pengesanan. Jika ia tiada, halaman
Ajar menyatakannya dan menunjuk kepada **Tetapan → AI**. Lihat
[Melatih model tersuai](training-models) untuk apa itu "runtime".

## Tiga jenis kemahiran {#kinds}

Bestari bertanya jenis kemahiran ini, dan jawapannya mengubah apa yang berlaku di sebalik tabir:

**Mengenali objek baharu.** Mengesan sesuatu di mana-mana dalam pandangan — uniform kurier, forklift,
trak syarikat. Inilah yang biasa.

**Membezakan baik daripada buruk.** Menilai item yang muncul di tempat yang sama — produk baik
berbanding cacat pada barisan pengeluaran. Ia menjangkakan benda tiba di tempat yang konsisten, dan
itulah yang menjadikan perbandingan itu bermakna.

**Mengesan apa-apa yang luar biasa.** Mempelajari rupa normal dan menandakan penyimpangan. Gunakan
ini apabila anda tidak boleh menyenaraikan apa yang anda cari — anda tahu apa yang *sepatutnya* ada
dan mahu tahu apabila sesuatu yang lain hadir.

## Menamakannya {#naming}

Terangkannya dengan perkataan anda sendiri: *penutup botol cacat*, *van syarikat*, *uniform kurier*.
Nama itu menjadi label yang akan anda lihat dalam amaran dan anda pilih dalam peraturan, jadi
jadikannya sesuatu yang anda akan kenali pada pemberitahuan.

Nama yang bertembung dengan label terbina dalam akan ditolak — pilih sesuatu yang lebih khusus. Itu
melindungi anda: kemahiran yang diajar bernama `person` tidak akan dapat dibezakan daripada pengesanan
model stok itu sendiri.

## Di mana ia muncul {#where}

Pilih kamera yang akan mempelajari kemahiran itu, kemudian lukis kotak di sekeliling tempat objek itu
muncul. Anda boleh membiarkan kotak itu kosong untuk memerhati keseluruhan pandangan.

Lukis kotak apabila benda itu memang muncul di satu tempat — penghantar, pintu masuk, ruang. Ia
menyempitkan apa yang perlu dipelajari dan meningkatkan hasil dengan ketara. Biarkan kosong apabila
benda itu boleh berada di mana-mana.

## Sesi mengajar {#sessions}

Setiap kemahiran mempunyai kad untuk mengumpul contoh: **sasaran** (yang sepatutnya menimbulkan
amaran) dan **normal / baik** (rupa kes biasa). Tekan mula pada satu kad dan tunjukkan contoh sebenar
kepada kamera; tangkapan dilabelkan secara automatik semasa ia tiba.

Jurulatih memberitahu anda apa lagi yang diperlukannya, dan berbaloi mengikut apa yang dikatakannya:

- **"Tunjukkan saya lebih banyak X"** — ia belum cukup contoh bagi kelas itu.
- **"Set-set itu tidak seimbang"** — satu kelas mempunyai jauh lebih banyak sampel daripada yang
  lain. Set mengajar yang berat sebelah menghasilkan kemahiran yang berat sebelah, yang menjawab
  dengan kelas yang lebih banyak dilihatnya.
- **"Tangkapan ini kelihatan sangat serupa"** — pelbagaikan sudut, kedudukan dan pencahayaan. Lima
  puluh gambar objek yang sama di tempat yang sama mengajar satu rupa sahaja, dan kemahiran itu gagal
  pada setiap rupa yang lain.

Semak jalur filem dan buang tangkapan yang buruk. Contoh yang tersalah label lebih teruk daripada
contoh yang tiada.

## Kemahiran dan peraturan {#rules}

Kemahiran yang diajar, sebaik ia aktif, menjadi sesuatu yang boleh dikesan peraturan seperti kelas
lain. Peraturan yang dicipta untuk kemahiran yang diajar ditanda sedemikian pada halaman Pengesanan
AI kamera, supaya anda boleh nampak sepintas lalu peraturan mana yang datang daripada pengajaran dan
bukan ditulis dengan tangan.

## Menguruskan model di sebaliknya {#advanced}

Mengimport, mengaktifkan dan membuang fail model terlatih — dan kelas objek yang dibawanya — berada
dalam **Tetapan → AI**. Itu juga tempat model yang dilatih di luar produk ini diimport. Lihat
[Melatih model tersuai](training-models).
