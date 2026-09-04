---
title: Bestari persediaan kali pertama
category: getting-started
categoryLabel: Permulaan
summary: Enam langkah daripada satah kawalan kosong kepada yang berfungsi — dan yang mana boleh dilangkau.
order: 30
---

# Bestari persediaan kali pertama

Bestari berjalan sekali, kali pertama seorang pentadbir log masuk, dan melalui tujuh langkah:
Selamat datang, Pengedaran, Log masuk, Tapak pertama, Tambah nod, Penyerahan, Selesai.

**Setiap langkah boleh dilangkau**, dan segala yang dilakukannya tersedia kemudian dari halaman
biasa. Tujuannya ialah mengeluarkan satah kawalan daripada keadaan kosong, bukan mengutip setiap
keputusan daripada anda di awal. Setiap langkah mengambil kurang seminit.

## Selamat datang {#welcome}

Mengesahkan siapa anda log masuk dan sama ada kata laluan permulaan telah ditukar, kemudian
meringkaskan apa yang dilakukan langkah selebihnya. Tiada apa untuk diisi.

## Pengedaran {#deployment}

Menentukan sama ada pemasangan ini ialah satu pelayan tunggal atau salah satu daripada beberapa
di belakang pengimbang beban, dan memberitahu anda sama ada ia benar-benar disediakan untuk yang
kedua itu.

Kebanyakan jawapannya bukan di dalam produk. Menghalakan cache dan kunci transaksi ke Redis ialah
separuh yang mudah; separuh yang diam-diam merosakkan pengedaran berada di luarnya — pengimbang
beban yang menamatkan TLS pada port yang sijil pelanggan nod *itulah* pengesahannya, kunci
penyulitan yang berbeza antara instans sehingga satu tidak dapat membaca apa yang dimeteraikan
oleh yang lain, rahsia tandatangan yang dicipta sendiri oleh setiap replika. Tiada satu pun
daripadanya mengumumkan dirinya. Ia muncul sebagai log masuk yang tidak menentu, atau sebagai
armada yang kelihatan sihat sehingga sesuatu membaca satu baris.

Jadi langkah ini melaporkan apa yang boleh disahkannya dari dalam — dengan nilai yang anda perlu
bandingkan antara instans — dan menyenaraikan dengan jelas apa yang hanya boleh disemak oleh
manusia di luar.

Apabila semakan gagal pada sesuatu yang *boleh* diperbetulkan oleh produk — cache dan kunci
transaksi yang masih setiap proses — langkah ini membolehkan anda menghalakannya ke Redis pada
ketika itu juga: masukkan alamat, uji sambungan, dan terapkan. Tetapan itu dibaca hanya semasa
permulaan, jadi menerapkannya akan menawarkan untuk memulakan semula instans. Halaman menunggu
proses baharu menjawab kemudian memuat semula dirinya: konfigurasi yang telah disimpan tetapi
belum dimuatkan adalah lebih buruk daripada yang belum disimpan, kerana skrin berkata ia sudah
ditetapkan.

## Log masuk {#signin}

Mengarahkan satah kawalan kepada pelayan identiti anda, supaya orang log masuk dengan akaun yang
sudah mereka miliki.

Import berkas SSO yang dieksport halaman Apps MyIDSan untuk satah kawalan ini, dan pengeluar,
audiens, ID klien dan URL ubah hala akan diisi serta disimpan tepat seperti yang ditulis MyIDSan —
bukan ditaip semula antara dua konsol, iaitu tempat perkara ini menjadi salah.

**Tiada pelayan identiti?** Langkau. Akaun permulaan tempatan yang anda gunakan terus berfungsi, dan
anda boleh menyambungkan satu kemudian dari Tetapan.

## Tapak pertama {#site}

Mencipta tempat untuk meletakkan peranti anda.

Sebuah **tapak** ialah tempat fizikal dengan kedudukan pada peta. Tanpanya, nod yang diambil tiada
tempat untuk duduk dan peta tiada apa untuk ditunjukkan. Namakannya seperti orang di organisasi anda
merujuknya — *Depoh Utara*, *Ibu Pejabat* — kerana nama itu muncul bersebelahan setiap amaran yang
datang daripadanya.

Pelan lantai di dalam tapak datang kemudian, dari halaman Peta; langkah ini hanya memerlukan tempat
itu wujud.

## Tambah nod {#node}

Mengambil peranti pertama anda. Inilah langkah yang menjadi sebab kewujudan seluruh satah kawalan,
dan satu-satunya yang mempunyai prasyarat: **nod mesti sudah memegang kunci armada anda.**

Ringkasnya: jana kunci armada di sini, tampalkannya ke dalam tetapan Connectivity nod itu sendiri,
jana kod tuntutan pada nod, dan masukkannya di sini. Butiran penuh — termasuk apa yang perlu
dilakukan apabila penemuan tidak menjumpai apa-apa — ada dalam [Mengambil nod](adopting-nodes).

Langkau jika peranti belum bersedia. Tiada apa lagi dalam bestari yang bergantung padanya.

## Penyerahan {#handoff}

Mengalihkan anda daripada akaun permulaan.

Akaun yang anda gunakan dicipta supaya anda boleh masuk. Ia dikongsi, ia tidak boleh dikaitkan
dengan seseorang, dan setiap tindakannya mendarat dalam log audit di bawah nama yang tidak mengenal
pasti sesiapa. Langkah ini ialah tempat anda menaikkan akaun sebenar kepada superadmin dan
menyingkirkan akaun permulaan.

Ia langkah yang paling mungkin dilangkau dan yang paling berbaloi dilakukan. Satah kawalan yang
masih dijalankan enam bulan kemudian daripada `superadmin` mempunyai log audit yang tidak dapat
menjawab satu-satunya soalan yang menjadi sebab kewujudannya.

## Selesai {#done}

Meringkaskan apa yang telah disediakan dan menutup bestari. Ia tidak muncul semula.

## Apa selepas ini {#next}

Bestari meninggalkan anda dengan satah kawalan yang berfungsi, bukan yang siap. Langkah lazim
seterusnya:

- **Ambil peranti anda yang selebihnya** — [Mengambil nod](adopting-nodes).
- **Beri peta sesuatu untuk ditunjukkan** — tambah pelan lantai pada tapak anda dan letakkan nod di
  atasnya.
- **Cipta peranan yang organisasi anda benar-benar ada**, dan berhenti mengedarkan superadmin. Lihat
  [Lawatan ruang kerja](workspace-tour#menu-differences).
