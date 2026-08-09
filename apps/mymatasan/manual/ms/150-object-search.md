---
title: Mencari apa yang dilihat kamera anda
category: daily-use
categoryLabel: Penggunaan harian
summary: Cari garis masa objek merentasi kamera dan tarikh — termasuk perkara yang tiada peraturan memberi amaran.
order: 150
---

# Mencari apa yang dilihat kamera anda

Carian objek menjawab soalan yang log amaran tidak boleh: *apa yang dilihat kamera ini, sama ada
mana-mana peraturan mengambil kisah atau tidak?*

Peraturan hanya memberitahu anda tentang apa yang terfikir untuk anda tanyakan lebih awal. Carian
objek merekodkan segala yang dikenali pengesan — orang, kenderaan, apa sahaja yang dihasilkan model
aktif anda — sebagai garis masa yang boleh dicari, supaya anda boleh mencari selepas kejadian.

Inilah alat untuk "sebuah van berada di sini pada suatu petang Selasa dan tiada peraturan memerhati
van".

## Menghidupkannya {#enabling}

Rakaman objek dibuat bagi setiap kamera, pada tab **Rakaman** kamera itu: **Rakam metadata objek**.

Ia menggunakan semula pengesan yang sudah pun berjalan, jadi kosnya hampir tiada tambahan, dan ia
berfungsi sama ada rakaman video dihidupkan atau tidak. Apabila rakaman memang wujud, setiap hasil
berpaut terus kepadanya.

> [!NOTE]
> Metadata hanya dirakam bermula dari saat anda menghidupkannya. Ia tidak boleh diisi ke belakang —
> tiada apa yang boleh digunakan untuk membinanya semula. Hidupkannya untuk kamera yang penting
> sebelum anda memerlukannya, bukan selepas.

## Mencari {#searching}

Tapis mengikut julat tarikh, kamera (atau semua kamera), jenis objek (atau mana-mana), dan keyakinan
minimum. Hasilnya ialah penampakan, terbaharu dahulu, dengan **Main rakaman** apabila rakaman
merangkumi detik itu dan **Tiada rakaman** apabila tiada.

Mulakan lebih luas daripada yang terasa munasabah. Mana-mana objek, semua kamera, sehari penuh —
kemudian sempitkan. Objek yang anda cari sering dikelaskan sebagai sesuatu yang bersebelahan dengan
apa yang anda jangkakan (van sebagai `truck`, basikal sebagai `motorcycle`), dan penapis yang ketat
menyembunyikan tepat hasil yang anda mahukan.

## Keyakinan {#confidence}

Setiap penampakan membawa keyakinan pengesan. Menaikkan minimum membuang penampakan yang tidak pasti
— dan membuang yang sebenar tetapi dirakam dengan buruk.

Semasa mencari selepas insiden, tetapkannya rendah. Hasil palsu memakan dua saat untuk diketepikan;
hasil yang terlepas memakan carian anda.

## Penampakan, bukan bingkai {#sightings}

Objek yang kekal dalam pandangan ialah satu entri, bukan satu bagi setiap bingkai. Objek yang muncul
semula dalam **tempoh sejuk penampakan** (lima saat secara lalai, boleh dikonfigurasikan bagi setiap
kamera) menyambung entri yang sama dan bukan memulakan yang baharu.

Itulah sebabnya seseorang yang berjalan melintasi tempat letak kereta menghasilkan satu baris dan
bukan empat ratus — dan sebab melangkah sebentar di belakang tiang tidak memecahkan mereka menjadi
dua orang.

## Apa ia bukan {#limits}

- **Ia bukan peraturan.** Tiada apa yang memberi amaran. Ia merekod supaya anda boleh mencari
  kemudian.
- **Ia tidak lebih baik daripada model.** Ia tahu tepat apa yang dikenali model pengesanan aktif, dan
  tiada apa lagi. Label yang tidak dihasilkan oleh mana-mana model aktif tidak akan sekali-kali
  muncul — lihat [Bagaimana pengesanan berfungsi](how-detection-works).
- **Ia bukan rakaman video.** Jika rakaman dimatikan, anda dapat penampakan tanpa video. Itu masih
  berguna — mengetahui sebuah kenderaan berada di pintu pagar pada 14:32 menyempitkan carian dengan
  ketara — tetapi ia bukan bukti dengan sendirinya.
