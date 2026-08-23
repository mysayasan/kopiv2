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

## Cari yang serupa {#appearance}

Mana-mana baris orang atau kenderaan membawa tindakan **Cari yang serupa**. Pilih satu penampakan
dan carian menyusun setiap penampakan lain yang direkodkan kamera itu mengikut sejauh mana ia
*kelihatan serupa* dengan yang anda pilih — warna pakaian, bentuk badan, bentuk dan warna kenderaan.

Ia suis berasingan daripada metadata objek, pada tab **Rakaman** yang sama: **Huraikan rupa untuk
carian**. Ia memerlukan rakaman metadata dihidupkan terlebih dahulu — huraian itu menumpang metadata
penampakan yang dicipta — dan ia tidak percuma seperti metadata. Ia satu laluan model bagi setiap
orang atau kenderaan pada setiap bingkai yang disampel, di atas pengesan itu sendiri. Hidupkannya
untuk kamera yang anda jangka akan ditanya "ke mana lagi ia pergi".

> [!NOTE]
> Tiada apa dihuraikan sehingga anda menghidupkan ini, dan tiada apa yang direkodkan sebelum saat
> itu boleh dijumpai olehnya — had yang sama seperti metadata objek itu sendiri.

### Apa maksud hasil bersusun, dan apa yang bukan {#appearance-scoring}

Hasil yang tersusun tinggi bukan pengecaman identiti. Ia datang daripada huraian imej tujuan umum,
bukan model pengecaman semula wajah atau tubuh, jadi ia baik membezakan rupa kasar — jaket merah
daripada jaket hitam, van daripada hatchback — dan jauh lebih lemah mengecam individu yang sama
merentasi perubahan besar dalam postur, cahaya atau kamera. Setiap hasil adalah senarai pendek untuk
*anda* sahkan dengan mata, bukan sekali-kali keputusan muktamad, dan skrin tidak pernah mendakwa
sebaliknya.

Tiada juga peratusan padanan, dan itu sengaja bukan tertinggal. Diukur terhadap model sebenar, dua
gambar orang yang *sama* mendapat kira-kira 98%, dan dua gambar dua orang yang *berbeza* mendapat
kira-kira 95% — angka mentah hampir tidak berubah tidak kira siapa yang dibandingkan, jadi
memaparkannya sebagai peratusan akan kelihatan seperti hampir pasti pada setiap baris. Skrin
sebaliknya menunjukkan sejauh mana hasil itu **menonjol** berbanding segala yang lain dibandingkan
bagi carian itu: hasil yang jelas mengatasi orang ramai berbaloi dilihat, yang hampir tidak berbuat
demikian tidak, tidak kira apa yang dibaca angka asas. Dengan penampakan yang terlalu sedikit untuk
dibandingkan, menonjol bermakna sedikit, dan skrin menyatakan demikian dan bukan mereka-reka
kedudukan daripada bukti yang terlalu sedikit.

## Apa ia bukan {#limits}

- **Ia bukan peraturan.** Tiada apa yang memberi amaran. Ia merekod supaya anda boleh mencari
  kemudian.
- **Ia tidak lebih baik daripada model.** Ia tahu tepat apa yang dikenali model pengesanan aktif, dan
  tiada apa lagi. Label yang tidak dihasilkan oleh mana-mana model aktif tidak akan sekali-kali
  muncul — lihat [Bagaimana pengesanan berfungsi](how-detection-works).
- **Ia bukan rakaman video.** Jika rakaman dimatikan, anda dapat penampakan tanpa video. Itu masih
  berguna — mengetahui sebuah kenderaan berada di pintu pagar pada 14:32 menyempitkan carian dengan
  ketara — tetapi ia bukan bukti dengan sendirinya.
