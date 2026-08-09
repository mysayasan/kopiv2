---
title: Konfigurasi rakaman
category: recording
categoryLabel: Rakaman & storan
summary: Hidupkan rakaman bagi setiap kamera dan tetapkan segmen, pra-gulung, pengekalan dan storan.
order: 410
---

# Konfigurasi rakaman

Rakaman dikonfigurasikan **bagi setiap kamera**, pada tab **Rakaman** kamera itu. Tiada suis global —
sesuatu tapak biasanya mahukan rakaman berterusan dari pintu pagar dan langsung tiada dari bilik
mesyuarat.

## Menghidupkannya {#enabling}

**Dayakan rakaman untuk kamera ini** memulakan rakaman berterusan. Segala di bawah hanya penting
setelah ia dihidupkan.

Rakaman bebas daripada pengesanan. Kamera boleh merakam tanpa sebarang peraturan, dan peraturan boleh
dicetuskan tanpa rakaman — amaran itu cuma tiada klip yang dilampirkan. Kebanyakan kamera mahukan
kedua-duanya.

## Panjang segmen {#segments}

Rakaman ditulis dalam segmen dengan bilangan minit tetap dan bukan satu fail tanpa penghujung.

Segmen yang lebih pendek pulih lebih baik daripada ranap atau gangguan kuasa — anda kehilangan paling
banyak segmen yang sedang berjalan. Segmen yang lebih panjang menghasilkan fail yang lebih sedikit.
Lalai ialah imbangan yang munasabah; jika tapak anda kerap kehilangan kuasa, pendekkannya.

## Pra-gulung dan pasca-gulung {#rolls}

Apabila peraturan dicetuskan, **klip peristiwa** diekstrak di sekeliling pencetus: sekian saat
sebelumnya dan selepasnya.

Pra-gulung yang penting. Bahagian menarik sesuatu insiden hampir selalu apa yang berlaku dalam
saat-saat *sebelum* pengesanan — bagaimana mereka tiba, dari mana. Pra-gulung beberapa saat menukar
klip seseorang berdiri di pintu menjadi klip mereka berjalan menuju ke situ.

## Pengekalan {#retention}

Berapa hari rakaman daripada kamera ini disimpan. Selepas itu, ia dibersihkan secara automatik untuk
memberi ruang.

Tetapkannya bagi setiap kamera, berdasarkan akibat dan bukan saiz cakera:

- Pintu pagar atau kaunter tunai di mana insiden muncul berminggu kemudian mahukan pengekalan panjang.
- Koridor yang tiada sesiapa pernah menyemaknya mahukan yang pendek.

Soalan yang perlu ditanya ialah *berapa lama sebenarnya seseorang di sini mengambil masa untuk
melihatnya?* Apa-apa yang lebih pendek daripada itu bermakna jawapan kepada "boleh kita lihat?"
lazimnya tidak. Lihat [Storan dan kapasiti](storage-and-capacity).

## Laluan storan {#storage}

Tempat rakaman ditulis. Halakannya ke pemacu data mesin, bukan pemacu sistem.

Menukarnya kemudian tidak memindahkan rakaman sedia ada. Betulkannya awal — bestari bertanya tepat
untuk itu.

Tab memberi amaran apabila volum yang dipilih hampir penuh, dan ia wajar dipercayai: rakaman berhenti
apabila cakera berhenti.

## Metadata objek {#metadata}

**Rakam metadata objek** merekodkan apa yang dilihat kamera sebagai garis masa yang boleh dicari,
bebas daripada sama ada rakaman disimpan. Lihat
[Mencari apa yang dilihat kamera anda](object-search).

**Jurang kehadiran** dan **tempoh sejuk penampakan objek** mengawal cara penampakan dikumpulkan:
objek yang muncul semula dalam tempoh sejuk menyambung entri yang sama dan bukan memulakan yang
baharu. Lalainya lima saat, yang mengekalkan seorang yang melintasi tempat letak kereta sebagai satu
baris dan bukan beratus-ratus.

## Apabila cakera penuh {#disk-full}

Dua kelakuan tersedia, dan pilihannya ialah keputusan polisi, bukan teknikal:

- **Tulis ganti rakaman tertua** — rakaman diteruskan, dan bahan tertua dibuang tanpa mengira tetapan
  pengekalannya. Anda sentiasa mempunyai rakaman terkini.
- **Jeda** — rakaman berhenti sehingga ruang dibebaskan. Anda menyimpan segala yang anda ada, dan
  anda tidak merakam apa-apa yang baharu.

Kebanyakan tapak mahukan tulis ganti: perakam yang senyap-senyap berhenti Selasa lepas lebih teruk
daripada perakam yang memegang sedikit kurang sejarah. Pilih secara sengaja, kerana kegagalan itu
kelihatan sama sekali berbeza dalam setiap kes.

## Membersihkan {#purging}

**Bersihkan yang tamat tempoh** memadam hanya rakaman yang sudah melepasi pengekalannya —
penyelenggaraan, selamat dijalankan.

**Bersihkan sekarang** memadam *semua* rakaman dan gambar petikan AI bagi sesuatu kamera tanpa
mengira pengekalan. Ia berjalan di sebalik kiraan detik yang boleh dibatalkan dan tidak boleh
dibatalkan selepas itu. Pentadbir sahaja.

## Penyulitan {#encryption}

Rakaman, gambar petikan dan imej latihan disulitkan pada cakera secara lalai. Ia telus — tiada apa
yang perlu dilakukan semasa merakam atau main balik — tetapi ia mengubah maksud set semula kilang dan
menjadikan kunci pemulihan sesuatu yang tidak boleh anda hilangkan. Lihat
[Penyulitan semasa rehat](encryption-at-rest).
