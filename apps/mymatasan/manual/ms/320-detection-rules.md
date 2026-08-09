---
title: Mencipta peraturan pengesanan
category: detection
categoryLabel: Pengesanan & AI
summary: Pilih mod, pilih apa yang hendak dikesan, lukis zon dan garis, tetapkan jadual, dan halakan amarannya.
order: 320
---

# Mencipta peraturan pengesanan

Peraturan ialah arahan tetap pada satu kamera: *dalam mod ini, memerhati benda-benda ini, di kawasan
ini, sepanjang waktu ini, beritahu saya — dan beritahu destinasi ini.*

Peraturan berada pada halaman **Pengesanan AI** sesuatu kamera. Satu kamera boleh mempunyai seberapa
banyak yang anda perlukan, dan menjalankan beberapa peraturan yang sempit biasanya lebih baik
daripada satu yang luas.

## Mod — bagaimana ia memerhati {#modes}

| Mod | Dicetuskan apabila |
|---|---|
| **Kehadiran** | Objek berada di mana-mana dalam pandangan (atau dalam zon). |
| **Orang ramai / kiraan** | Sekurang-kurangnya *N* orang berada dalam zon dalam satu bingkai. |
| **Pencerobohan (zon)** | Objek memasuki kawasan yang dilukis. |
| **Lintasan garis** | Objek melintasi garis yang dilukis, boleh dihadkan kepada satu arah. |
| **Lintasan berbilang garis** | Objek melintasi beberapa garis mengikut urutan, dalam had masa. |
| **Plat nombor (LPR)** | Plat yang boleh dibaca dilihat — lihat [Api, asap dan plat](fire-smoke-and-plates#lpr). |

Pilih mengikut soalan yang sebenarnya anda tanya. "Adakah ada sesiapa di halaman?" ialah kehadiran.
"Adakah ada sesiapa masuk melalui pintu pagar?" ialah lintasan garis — dan ia tidak akan dicetuskan
pada orang yang sudah pun berdiri di situ, iaitu biasanya apa yang anda mahukan.

**Lintasan berbilang garis** ialah yang wajar diketahui: dua garis dilintasi mengikut urutan dalam
had masa menyatakan arah pergerakan melalui sesuatu ruang, yang menapis keluar orang yang melepak,
mundar-mandir dan orang lalu-lalang yang satu garis tunggal tidak boleh.

## Kesan — apa yang ia perhatikan {#detect}

Pilih kelas objek. Biarkan ia sebagai **apa sahaja** dan setiap objek yang dikesan akan sepadan, yang
merupakan cara yang baik untuk melihat apa yang sebenarnya dihasilkan kamera dan cara yang buruk
untuk menjalankan sesuatu tapak.

Kelas datang daripada daftar — lihat [Kelas dan kumpulan objek](object-classes). Kelas yang ditanda
**model tidak aktif** tidak boleh sepadan dengan apa-apa: model yang menghasilkan labelnya tidak
berjalan.

## Zon {#zones}

Lukis kawasan yang dipedulikan peraturan pada pratonton kamera. Satu peraturan boleh mempunyai
**beberapa zon**, dan ia bergabung sebagai "mana-mana daripadanya".

Ini penalaan bernilai paling tinggi dalam produk ini. Kamera yang memerhati pintu pagar hampir
selalu turut memerhati laluan pejalan kaki awam, dan peraturan tanpa zon memberi amaran pada setiap
pejalan kaki yang lalu di hadapan hartanah anda. Melukis di sekeliling pintu pagar tidak menjadikan
pengesan lebih baik — ia menjadikan peraturan itu bertanya soalan yang betul.

Pratonton menunjukkan berapa peratus bingkai yang diliputi sesuatu zon. Alat membolehkan anda
menambah dan memadam titik, memusatkan kotak, membalikkan, memutar, mengancing pada grid, dan
membuat asal.

Ingat bahawa zon ialah kawasan pada *bingkai*, bukan di dunia. Pan atau pasang semula kamera dan zon
itu kini menutup tempat lain sama sekali.

## Garis {#lines}

Bagi mod lintasan, lukis garis dan tetapkan arahnya. Anak panah hijau menunjukkan arah lintasan yang
mencetuskan, dan bahagian berlorek menandakan sisi pencetus. Klik anak panah untuk berkitar: satu
arah → arah satu lagi → kedua-dua arah.

Letakkan garis di tempat sesuatu *mesti* melalui, bukan di tempat ia *mungkin* melalui: pintu pagar,
pintu masuk, leher koridor. Garis merentasi tanah lapang menangkap satu laluan dan terlepas tiga yang
lain.

Bagi berbilang garis, tetapkan **saat maksimum antara garis**. Terlalu pendek dan pejalan yang
perlahan tidak pernah melengkapkan urutan; terlalu panjang dan dua peristiwa tidak berkaitan yang
berselang sejam menjadi satu amaran. Ambil masa seseorang benar-benar berjalan melaluinya.

## Keyakinan, bingkai dan tempoh sejuk {#tuning}

**Ambang**, **bingkai minimum** dan **tempoh sejuk** — semuanya dijelaskan dalam
[Bagaimana pengesanan berfungsi](how-detection-works#confidence).

Urutan untuk menala: betulkan zon dahulu, kemudian bingkai minimum, kemudian ambang, kemudian tempoh
sejuk. Kebanyakan orang mencapai ambang dahulu, dan ia yang paling kasar antara keempat-empatnya.

## Jadual {#schedule}

Peraturan boleh berjalan **sentiasa**, atau mengikut jadual: siang, malam, hari bekerja, hujung
minggu, corak mingguan tersuai, atau julat tarikh tertentu.

**Mod polisi** ialah bahagian yang perlu dibaca dua kali:

- **Kesan hanya semasa jadual ini** — peraturan aktif di dalamnya dan senyap di luarnya.
- **Jeda semasa jadual ini** — peraturan aktif *kecuali* di dalamnya.

Yang kedua ialah cara anda menyatakan "beri amaran di halaman, tetapi bukan semasa syif pagi
memunggah". Jadual mempunyai tetapan zon waktunya sendiri, jadi tapak di zon berbeza daripada peranti
berkelakuan seperti yang dijangka operatornya.

## Penghalaan pemberitahuan {#routing}

Secara lalai amaran sesuatu peraturan pergi ke setiap destinasi yang dikonfigurasikan. Pilih yang
tertentu untuk menghalakannya — "peraturan ruang muatan pergi ke topik MQTT gudang, bukan ke telefon
semua orang pada 3 pagi".

Tanpa sebarang destinasi dikonfigurasikan, amaran masih muncul dalam suapan dalam aplikasi; cuma
tiada tempat lain untuknya pergi. Lihat [Destinasi pemberitahuan](notification-destinations).

**Amaran bunyi** memainkan bunyi dalam pelayar sesiapa yang sedang memerhati. Gunakannya pada
beberapa peraturan yang wajar dipandang serta-merta, dan tidak di tempat lain — bunyi yang berbunyi
berterusan ialah bunyi yang orang bisukan.

## Mendayakan, melumpuhkan dan membaca hasilnya {#lifecycle}

Peraturan boleh dilumpuhkan tanpa dipadam, dan itulah cara yang betul untuk menguji sesuatu teori.

Sebaik peraturan berjalan, **Log Amaran** pada halaman yang sama ialah tempat anda menilainya: tapis
mengikut masa, peristiwa, keyakinan atau keadaan. Sehari amaran sebenar memberitahu anda lebih banyak
tentang sesuatu ambang berbanding sebanyak mana pun penaakulan.

## Peraturan pertama yang praktikal {#first-rule}

Bagi kebanyakan tapak, pada kebanyakan kamera:

1. Mod **Pencerobohan (zon)**.
2. Kesan **Orang**.
3. Zon dilukis mengelilingi hartanah anda sahaja — bukan laluan pejalan kaki, bukan jalan raya.
4. Ambang lalai, bingkai minimum **2**, tempoh sejuk sekitar **30** saat.
5. Jadual **Sentiasa** pada permulaan.

Jalankannya sehari, baca log amaran, kemudian sempitkan. Urutan itu mengatasi cubaan menetapkannya
dengan betul lebih awal, kerana anda tidak akan berjaya.
