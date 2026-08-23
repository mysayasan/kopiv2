---
title: Mencari dan memainkan rakaman
category: daily-use
categoryLabel: Penggunaan harian
summary: Cari rakaman mengikut kamera dan masa, lompat terus ke peristiwa, dan eksport klip.
order: 140
---

# Mencari dan memainkan rakaman

Halaman Rakaman ialah main balik NVR berterusan, dengan klip peristiwa dipilih daripadanya.

Pemerhati tidak boleh membuka halaman ini. Menyemak rakaman ialah garis antara pemerhati dan operator
— lihat [Apa itu MyMataSan](welcome#roles).

## Mencari rakaman {#finding}

Pilih kamera dan tarikh. Garis masa bagi hari itu menunjukkan apa yang wujud: bahagian **berterusan**
di mana rakaman berjalan, dan **klip peristiwa** di mana sesuatu peraturan dicetuskan. Klik di
mana-mana padanya untuk melompat ke masa itu.

Jurang pada garis masa bersifat memberitahu dan bukan rosak. Jurang bermakna rakaman tidak berjalan —
kamera luar talian, rakaman dimatikan, atau peranti tidak beroperasi. Berbaloi mengetahui yang mana
satu, dan [Kebolehpercayaan kamera](dashboard#reliability) biasanya menjawabnya.

## Main semula garis masa {#timeline}

**Garis Masa**, di sebelah Paparan Langsung pada bar navigasi, memainkan rakaman mengikut jam dan
bukan mengikut fail: luncur di mana-mana pada bar luncur dan main semula merentas sempadan segmen
dengan sendirinya — tanpa perlu mencari dan membuka klip seterusnya secara manual. Lorekan pada bar
itu ialah rakaman yang sama yang dilaporkan oleh jalur liputan di atas, jadi kedua-duanya tidak
akan berbeza.

Tonton sehingga **8 kamera** pada detik yang sama, diselaraskan, pada kelajuan antara **0.25x**
hingga **8x**. Jika sesuatu peraturan tercetus dalam tempoh itu, peristiwanya dipetakan pada bar
sebagai tanda yang boleh diklik — klik satu untuk melompat terus kepadanya, jalan pintas yang sama
seperti yang diterangkan dalam [Melompat daripada amaran](#from-alert) dari sisi pemberitahuan.
Tanda memerlukan halaman Amaran selain Rakaman; tanpanya bar masih boleh diluncur, cuma tanpa tanda.

Meluncur ke detik yang tiada rakaman tidak meneka secara senyap: main semula memberitahu anda dan
melangkau ke hadapan ke detik seterusnya yang mempunyai rakaman, serta memberitahu berapa banyak
yang dilangkau untuk sampai ke sana.

## Melompat daripada amaran {#from-alert}

Laluan yang jauh lebih biasa ialah arah bertentangan: anda ada amaran dan mahukan rakamannya. Gunakan
**Lihat klip** pada pemberitahuan, yang mendarat pada detik peraturan itu dicetuskan dan bukan
memaksa anda memburunya.

Klip peristiwa merangkumi **pra-gulung** dan **pasca-gulung** — bilangan saat yang boleh
dikonfigurasikan sebelum dan selepas pencetus — kerana apa yang berlaku sejurus sebelum pengesanan
biasanya bahagian yang anda perlukan.

## Memainkan {#playing}

Main balik berfungsi seperti mana-mana pemain video. Apabila rakaman itu dalam kodek yang pelayar
tidak boleh main terus, peranti menukarkannya semasa dihantar keluar; itu memerlukan CPU semasa anda
menonton tetapi bermakna main balik berfungsi tanpa memasang apa-apa.

## Mengeksport {#exporting}

Muat turun klip yang sedang anda lihat. Itu memberi anda fail yang boleh diserahkan kepada seseorang
yang tiada akses ke peranti — dan itulah tujuannya.

Dua perkara yang perlu diketahui sebelum anda bergantung padanya:

- Muat turun ialah rakaman sebagaimana ia dirakam, tanpa tera air atau tandatangan. Anggap rantaian
  jagaan sebagai urusan prosedur di tapak anda, bukan sesuatu yang dibuktikan oleh fail itu sendiri.
- Rakaman disulitkan pada cakera. Fail yang dieksport **tidak** — ia video biasa. Uruskannya
  sewajarnya.

## Pengekalan, dan mengapa rakaman hilang {#retention}

Setiap kamera mempunyai tempoh pengekalan, dan rakaman yang melepasinya dibersihkan secara automatik
untuk memberi ruang kepada rakaman baharu. Itu bukan kerosakan; itulah yang menghalang cakera
terhad daripada penuh.

Akibat praktikalnya: **insiden yang tiada sesiapa melihatnya dalam tempoh pengekalan akan hilang.**
Jika kitaran semakan tapak anda mingguan, pengekalan tujuh hari sudah pun terlalu pendek. Tetapkan
pengekalan daripada berapa lama sebenarnya seseorang mengambil masa untuk melihatnya, bukan daripada
berapa banyak cakera yang kebetulan anda ada. Lihat [Storan dan kapasiti](storage-and-capacity).

## Membersihkan {#purging}

Pentadbir mempunyai dua kawalan pembersihan, dan kedua-duanya sangat berbeza:

- **Bersihkan yang tamat tempoh** memadam hanya rakaman yang sudah melepasi pengekalannya. Rakaman
  dalam tempoh pengekalan dikekalkan. Ini kerja penyelenggaraan, dan ia selamat.
- **Bersihkan sekarang** memadam **semua** rakaman dan gambar petikan AI bagi sesuatu kamera tanpa
  mengira pengekalan. Ia tidak boleh dibatalkan, dan ia berjalan di sebalik kiraan detik yang boleh
  anda batalkan.

Operator tiada kedua-duanya. Operator yang berada di lokasi semasa sesuatu insiden tidak boleh memadam
rakaman insiden itu, dan itu disengajakan.
