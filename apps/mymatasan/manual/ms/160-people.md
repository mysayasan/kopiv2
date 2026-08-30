---
title: Orang dan pengecaman wajah
category: daily-use
categoryLabel: Penggunaan harian
summary: Daftarkan orang supaya kamera mengenali mereka — dan tanggungjawab kebenaran yang datang bersamanya.
order: 160
---

# Orang dan pengecaman wajah

Pengecaman wajah membolehkan kamera anda memberitahu *siapa* dan bukan hanya *seseorang*.
Mendaftarkan seseorang berlaku serta-merta — tiada langkah latihan.

## Sebelum anda mendaftarkan sesiapa {#consent}

Peranti memaksa anda menerima ini sebelum ciri ini dihidupkan, dan ia bukan teks templat.

Mendaftarkan seseorang menyimpan **cap wajah**: perwakilan matematik bagi wajah mereka. Itu ialah
**data biometrik**. Di bawah GDPR, BIPA dan undang-undang setara di banyak bidang kuasa, anda pada
amnya memerlukan kebenaran termaklum orang itu *sebelum* anda mendaftarkan mereka, dan anda
bertanggungjawab atas cara ia digunakan selepas itu.

Peraturan praktikal yang mengekalkan anda di pihak yang betul:

- **Hanya daftarkan orang yang telah bersetuju.** Bukan "orang yang bekerja di sini". Bukan "orang
  yang kita ada gambarnya".
- **Beritahu mereka tujuannya.** Kebenaran untuk dikenali di pintu kakitangan bukan kebenaran untuk
  dijejaki merentasi tapak.
- **Padamkan orang yang telah pergi.** Memadam seseorang memadamkan cap wajah mereka.

Cap wajah disulitkan semasa rehat, dan memadam seseorang membuangnya. Itu melindungi data itu; ia
tidak mewujudkan kebenaran untuk memegangnya.

Jika anda tidak pasti anda mempunyai kebenaran, jangan daftarkan. Setiap ciri pengesanan lain dalam
produk ini berfungsi pada *apa* dan bukan *siapa*, dan tiada satu pun daripadanya membawa
tanggungjawab ini.

## Persediaan sekali sahaja {#setup}

Pengecaman wajah menggunakan dua fail model yang tidak disertakan bersama peranti — ia dilesenkan
secara berasingan dan model pengecaman bersaiz kira-kira 37 MB. Selagi ia tiada pada mesin,
pendaftaran foto akan ditolak.

Anda tidak memerlukan gesaan arahan untuk ini. Skrin Orang memaparkan panel **Pengecaman wajah
memerlukan persediaan sekali sahaja** dengan butang **Muat turun dan sediakan** apabila ada sesuatu
yang tiada; kawalan yang sama ada di **Tetapan › AI**. Ia mengambil hanya apa yang tiada, memasang
pakej `opencv-python` jika masa jalan AI tidak memilikinya, memastikan model benar-benar dimuatkan,
dan memaparkan lognya semasa ia berjalan. Tiada apa-apa perlu dimulakan semula selepas itu —
daftarkan foto terus.

Ia memerlukan akses internet keluar ke `github.com` untuk muat turun. Pada peranti tanpa laluan
keluar, jalankan `ai/setup.ps1 -Faces` (atau `setup.sh`) pada mesin yang mempunyainya dan salin
kedua-dua fail `.onnx` ke folder yang dinamakan oleh panel itu.

Jika panel menyatakan **masa jalan AI** tiada, pasang ia dahulu (Tetapan › AI): model wajah
dimuatkan olehnya, jadi tiada tempat untuk model itu berjalan sehingga ia ada.

## Mendaftarkan {#enrolling}

Tambahkan seseorang mengikut nama, kemudian tambahkan gambar mereka. Menamakan seseorang sahaja
tidak melakukan apa-apa: orang tanpa gambar tiada dalam galeri yang dibaca pengecam, jadi mereka
tidak akan dipadankan. Senarai menyatakannya pada kad mereka, dan skrin membawa anda terus ke
gambar mereka apabila anda menambahkannya.

Terdapat dua cara untuk menambah gambar, bersebelahan dalam panel yang sama:

- **Dari komputer ini** — pilih atau seret masuk fail imej. Beberapa sekaligus juga boleh.
- **Ambil foto** — gunakan kamera pada komputer yang anda duduki. Pandang terus ke arahnya dan
  penuhi bingkai dengan wajah.

Apa pun caranya, hanya cap wajah dan keratan kecil wajah disimpan; gambar yang anda berikan tidak
disimpan. Setiap gambar yang didaftarkan muncul dalam panel bersama kualitinya, dan boleh dibuang
satu persatu — berbaloi dilakukan jika gambar yang buruk telah menyelinap masuk, kerana satu cap
wajah yang buruk merosakkan setiap padanan selepas itu.

Pelayar hanya menawarkan kamera pada alamat **HTTPS** atau pada **localhost**. Jika dibuka melalui
alamat LAN `http://` biasa, pilihan "Ambil foto" akan menyatakannya dan bukan kelihatan rosak; muat
naik gambar sebagai ganti, atau capai perakam melalui HTTPS.

Apa yang membuatkannya berfungsi:

- **Sepuluh hingga tiga puluh gambar.** Kurang pun boleh, tetapi kurang boleh dipercayai.
- **Sudut dan pencahayaan yang pelbagai.** Gambar yang semuanya kelihatan sama mengajar sistem satu
  rupa sahaja, dan ia akan gagal pada rupa yang lain. Sertakan pencahayaan yang benar-benar ada pada
  kamera.
- **Tepat satu wajah besar bagi setiap gambar.** Gambar berkumpulan ditolak, dan wajah yang hanya
  beberapa piksel lebarnya tidak membawa butiran yang berguna.
- **Seluruh kepala dalam bingkai.** Gambar pasport, potret telefon dan fail kamera bersaiz penuh
  semuanya berfungsi, pada apa jua saiz — tetapi keratan yang terlalu ketat sehingga dagu atau bahagian
  atas kepala terpotong tidak memberi pengesan apa-apa untuk diproses, dan itulah satu-satunya bingkai
  yang masih ditolak.

Gambar yang diambil daripada kamera anda sendiri, di tempat pengecaman akan berlaku, mengatasi
gambar studio yang bagus. Padankan keadaannya, bukan kualitinya.

## Apa yang berlaku apabila seseorang dikecam {#what-happens}

Padanan bukan sekadar satu baris dalam log. Setiap satu ini berlaku, mengikut urutan:

1. **Satu amaran** ditulis, dilabelkan dengan nama orang itu dan keyakinan padanan — *Aminah Yusof
   (94%)* — atau *Wajah tidak dikenali* bagi seseorang yang tidak didaftarkan. Ia membawa petikan
   gambar dengan wajah dikotakkan.
2. **Klip peristiwa** dirakam sekitar detik itu, jika kamera tersebut sedang merakam.
3. **Pemberitahuan** dihantar ke loceng dan ke destinasi yang anda tetapkan (webhook, Telegram,
   MQTT), berserta petikan gambar. `{{person}}` boleh digunakan dalam templat pemberitahuan.
4. Secara pilihan, **kamera bergerak** ke kedudukan tersimpan dan **geganti dipicu** (siren, lampu
   denyar, pintu pagar) — kedua-duanya ditetapkan pada peraturan itu sendiri, dalam tab **Pengesanan
   AI** kamera.

Anda melihat hasilnya dalam **Pemberitahuan**, dalam log amaran kamera, dan pada Garis Masa — bukan
pada halaman Orang, sebab itu senarai juga memaparkan penampakan terkini setiap orang.

## Memilih apa yang perlu dimaklumkan {#alert-modes}

Peraturan wajah setiap kamera menanyakan satu daripada tiga soalan, dipilih di halaman Orang di
sebelah suis kamera:

- **Sesiapa yang didaftarkan** — beritahu saya apabila seseorang yang kita kenal berada di sini.
  Sesuai untuk pintu kakitangan.
- **Hanya orang yang dipilih** — beritahu saya apabila salah seorang daripada *mereka* berada di
  sini. Senarai perhatian; pilih nama di bawahnya. Peraturan tanpa sesiapa dalam senarai akan
  ditolak, kerana ia tidak akan memaklumkan sesiapa pun.
- **Wajah tidak dikenali (orang asing)** — beritahu saya apabila seseorang yang kita *tidak* kenal
  berada di sini. Sesuai untuk perimeter. Ia tidak menamakan sesiapa; ia melaporkan bahawa wajah
  yang tidak dikenali telah muncul, dan itulah yang jujur untuk dikatakan.

Kamera yang berbeza biasanya mahukan jawapan yang berbeza. Pilihan yang sama, berserta had keyakinan
dan tindakan penghalaan/geganti/PTZ, tersedia sepenuhnya dalam tab **Pengesanan AI** kamera.

## Memilih tempat ia berjalan {#per-camera}

Pengecaman didayakan **bagi setiap kamera**, bukan secara global. Ia berjalan hanya di tempat anda
menghidupkannya.

Kekalkan senarai itu pendek. Setiap kamera yang melakukan pengecaman memakan pemprosesan dan — lebih
penting — mengecam wajah di kamera kantin sedangkan anda hanya perlukan pintu masuk kakitangan ialah
tepat lebihan capaian yang menjadi topik perbualan kebenaran tadi. Dayakan ia di tempat yang ada
sebab dan tiada di tempat lain.

## Apabila pengecaman tidak boleh dipercayai {#accuracy}

Hampir selalu salah satu daripada ini, mengikut urutan ini:

1. **Gambar tidak cukup, atau gambar terlalu serupa.** Tambah lagi, yang pelbagai.
2. **Kamera tidak dapat melihat wajah.** Dipasang tinggi dan menghala ke bawah memberi anda bahagian
   atas kepala. Pengecaman memerlukan wajah lebih kurang mengadap depan dan agak besar dalam bingkai.
3. **Pencahayaan.** Cahaya belakang yang kuat — pintu masuk menentang cahaya siang — menjadikan semua
   orang bayang. Betulkan kedudukan kamera atau pencahayaan; sebanyak mana pun pendaftaran tidak
   mengimbanginya.

Anggap pengecaman sebagai petunjuk kuat, bukan bukti. Ia bukti untuk menyemak rakaman, bukan
kesimpulan untuk ditindaki dengan sendirinya.

## Memadam {#deleting}

Memadam seseorang membuang mereka dan semua cap wajah mereka, dan tidak boleh dibatalkan. Lakukannya
apabila seseorang pergi, atau menarik balik kebenaran — dan pastikan ada orang di tapak anda yang
memiliki tugas itu sebagai rutin, kerana tiada apa yang akan mengingatkan anda.
