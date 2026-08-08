---
title: Bestari persediaan kali pertama
category: getting-started
categoryLabel: Permulaan
summary: Apa yang dilakukan setiap satu daripada sembilan langkah persediaan, yang mana boleh dilangkau dengan selamat, dan apa yang perlu dibetulkan kemudian.
order: 30
---

# Bestari persediaan kali pertama

Bestari ini berjalan sekali sahaja, kali pertama pentadbir log masuk, dan melalui sembilan langkah:
Selamat Datang, Sistem, AI, Kapasiti, Kamera, Rakaman, Amaran, Ketersambungan, Selesai.

Tiada apa di sini yang kekal. Setiap tetapan yang dibuat oleh bestari boleh ditukar kemudian dalam
Tetapan atau pada halaman kamera, dan setiap langkah kecuali yang pertama boleh dilangkau. Tujuannya
ialah membawa anda daripada peranti kosong kepada kamera yang berfungsi dengan rakaman dan amaran
dalam satu pusingan, bukan untuk memerah setiap keputusan daripada anda pada awalnya.

Jalur langkah di bahagian atas menunjukkan di mana anda berada, dan **Langkau persediaan** di sudut
meninggalkan bestari untuk selamanya. Jika anda menutup pelayar di pertengahan jalan, bestari akan
menyambung semula di tempat anda berhenti.

## Selamat Datang {#welcome}

Mengesahkan siapa anda yang telah log masuk dan bahawa kata laluan anda telah ditetapkan. Jika anda
sampai ke sini tanpa dipaksa menukar kata laluan — kerana kata laluan dibekalkan melalui konfigurasi
— langkah ini menawarkan **Tukar kata laluan** supaya anda tetap boleh menetapkan kata laluan
sendiri.

Ia juga menawarkan **Pulih daripada sandaran**, yang merupakan laluan berbeza melalui keseluruhan
bestari: lihat [Memulihkan daripada sandaran](restore-from-backup). Ambil laluan itu hanya jika anda
mengambil alih konfigurasi pemasangan sedia ada ke mesin ini.

## Sistem {#system}

Memeriksa dua perkara yang bukan tanggungjawab MyMataSan untuk menyediakannya.

**Enjin video (ffmpeg).** Paparan langsung, rakaman dan bingkai yang dilihat AI semuanya melalui
ffmpeg. Jika ia tidak dijumpai, langkah ini menawarkan untuk memuat turun dan memasangnya untuk
anda. Pada platform yang muat turun automatik tidak tersedia, pasang ffmpeg sendiri dan tunjukkan
Tetapan → Runtime kepadanya. Tiada apa selepas ini berfungsi tanpanya, jadi ini satu-satunya langkah
yang tidak boleh dilangkau.

**Jam dan zon waktu.** Setiap amaran dan setiap saat rakaman dicap dengan jam hos. MyMataSan
melaporkan apa yang ditunjukkan jam itu tetapi sengaja tidak mengubahnya — perakam yang senyap-senyap
menulis semula masa sistem ialah perakam yang cap masanya tidak boleh dipercayai. Jika ia salah,
betulkannya dalam sistem pengendalian, kemudian kembali ke sini.

## AI {#ai}

Pengesan memerlukan dua perkara: runtime dan model.

**Runtime AI.** Python beserta pustaka pengesanan. **Pasang sokongan AI** mengambilnya. Ini muat
turun terbesar dalam persediaan dan yang paling mungkin mengambil beberapa minit.

**Model pengesanan.** Model yang lebih besar melihat lebih banyak dan berjalan lebih perlahan.
Langkah ini mencadangkan pilihan lalai yang munasabah untuk perkakasan yang ditemuinya. Pilih yang
lebih kecil jika mesin itu sederhana atau anda merancang untuk menjalankan banyak kamera, dan yang
lebih besar jika anda mempunyai GPU dan sedikit kamera.

Melangkau langkah ini memberi anda perakam tanpa pengesanan — kamera, paparan langsung dan rakaman
semuanya berfungsi, cuma anda tidak mendapat amaran. Anda boleh kembali ke Tetapan → AI pada
bila-bila masa.

## Kapasiti {#capacity}

Menjawab "berapa banyak kamera yang sebenarnya boleh ditanggung mesin ini?" sebelum anda menetapkan
jumlahnya.

**Anggaran pantas** membaca perkakasan dan memodelkannya. **Jalankan penentukuran** benar-benar
menanda aras pengesan pada hos ini dan jauh lebih tepat — berbaloi dengan seminit yang diambil, dan
paling baik dijalankan semasa mesin dalam keadaan senggang.

Hasilnya menamakan sumber yang *mengehadkan*: CPU, GPU, memori atau cakera. Itulah bahagian yang
berguna. Jika cakera ialah hadnya, nombor yang ditunjukkan ialah imbangan terhadap pengekalan dan
bukan siling keras — rakaman berputar, jadi cakera yang lebih kecil bermakna kurang hari disimpan,
bukan kurang kamera dibenarkan.

## Kamera {#cameras}

**Imbas rangkaian** mencari kamera ONVIF pada rangkaian tempatan dan menyenaraikan apa yang
ditemuinya.

Bagi setiap kamera yang anda mahukan, berikan nama yang anda benar-benar akan guna melalui radio —
*Pintu Depan*, *Ruang Muatan* — dan nama pengguna serta kata laluan kamera itu sendiri. Kebanyakan
kamera tidak akan menstrim tanpanya. Jika kelayakan itu salah, penambahan gagal di sini dan bukannya
kelihatan berjaya kemudian memaparkan jubin hitam.

Kamera yang tidak ditemui oleh imbasan itu tidak hilang; ia boleh ditambah melalui alamat kemudian.
Jangan habiskan masa untuknya sekarang.

## Rakaman {#recording}

Menghidupkan rakaman berterusan untuk kamera yang baru anda tambah, dengan pengekalan lalai **7
hari**.

Semak **folder storan** sebelum anda meneruskan. Langkah ini menunjukkan sejauh mana penuh volum itu
dan memberi amaran jika ia hampir penuh — rakaman berhenti apabila cakera penuh. Jika peranti
mempunyai pemacu data yang besar, tunjukkan ini kepadanya sekarang; memindahkan rakaman kemudian
lebih menyusahkan daripada memilih dengan betul di sini.

Jadual, kualiti dan pengekalan bagi setiap kamera ditala kemudian pada tab Rakaman setiap kamera.
Langkah ini sengaja hanya satu suis.

## Amaran {#alerts}

Menambah peraturan **orang** kepada setiap kamera — peraturan paling berguna di kebanyakan tapak —
dan, secara pilihan, tempat untuk menghantar amaran.

Tanpa destinasi, amaran tetap berlaku; ia muncul dalam suapan Pemberitahuan dalam aplikasi dan tidak
di tempat lain. Dengan destinasi, ia juga sampai kepada anda apabila tiada sesiapa memerhati skrin.
Bestari menawarkan tiga:

- **Webhook** — URL untuk dihantar POST. Pilihan serba guna.
- **Telegram** — token bot dan ID sembang.
- **MQTT** — URL broker dan topik.

Pengesahan, sijil klien TLS, penghalaan setiap peraturan dan penyusunan templat mesej semuanya
dikonfigurasikan kemudian dalam Tetapan → Pemberitahuan. Di sini anda hanya perlukan alamatnya.

## Ketersambungan {#connectivity}

Pilihan, dan hanya relevan jika peranti ini ialah satu nod daripada armada yang diuruskan daripada
satah kawalan MySeliaSan.

Tampal **kunci armada** daripada satah kawalan anda dan simpannya — nod itu menjadi boleh ditemui.
Kemudian **jana kod tuntutan** dan masukkan kod itu dalam satah kawalan untuk mengambil nod ini.

Jika anda tidak menjalankan satah kawalan, langkau ini. Jika anda tidak pasti, langkau juga: nod
boleh digandingkan kemudian daripada Tetapan → Ketersambungan tanpa mengulang apa-apa.

## Selesai {#done}

Merumuskan apa yang telah disediakan. **Selesai** menutup bestari, menanda persediaan kali pertama
sebagai lengkap, dan membuka papan pemuka anda dengan kamera yang anda tambah sudah pun disusun ke
dalam paparan langsung.

## Apa yang perlu dibuat selepas ini {#next}

Bestari meninggalkan anda dengan sistem yang berfungsi, bukan yang siap sepenuhnya. Langkah
seterusnya yang biasa ialah:

- Lukis zon pada peraturan yang penting, supaya kamera yang memerhati laluan pejalan kaki awam tidak
  memberi amaran tentang laluan itu.
- Semak profil strim setiap kamera jika paparan langsung kelihatan kabur atau tersekat-sekat.
- Tambah akaun yang akan digunakan rakan sekerja anda, pada peranan yang sepatutnya — lihat
  [Apa itu MyMataSan](welcome#roles) untuk maksud ketiga-tiga peranan itu.
