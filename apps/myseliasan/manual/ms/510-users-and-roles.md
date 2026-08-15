---
title: Pengguna, peranan dan akses
category: admin
categoryLabel: Pentadbiran
summary: Tiada siapa masuk sekadar dengan log masuk — dan menu serta kebenaran API datang dari satu tempat.
order: 510
---

# Pengguna, peranan dan akses

## Log masuk baharu tidak mendapat apa-apa {#pending}

Seseorang yang berjaya log masuk tiba **tanpa sebarang peranan** dan melihat skrin *akses menunggu*
dan bukan satah kawalan.

Itu disengajakan dan itulah bahagian penting halaman ini. Pengesahan membuktikan siapa seseorang
itu; ia tidak menyatakan apa yang boleh dilihatnya. Sehingga pentadbir menetapkan peranan, akaun
baharu tidak boleh membuka apa-apa — jadi pembekal identiti yang memperuntukkan akaun dengan bebas
tidak boleh senyap-senyap menyerahkan armada anda kepada sesiapa.

Membersihkannya ialah satu tindakan sahaja: pilih peranan dalam senarai **Users**.

## Mengurus pengguna {#users}

Senarai memaparkan **Kind** setiap akaun (akaun tempatan, atau akaun bersekutu melalui myidsan),
peranannya, dan sama ada ia aktif.

- **Tetapkan peranan** — tindakan pelepasan yang biasa.
- **Disable** — mengekalkan akaun dan menarik balik aksesnya. Inilah yang anda mahu untuk seseorang
  yang sudah berhenti; memadam akaun akan menghilangkan jejak apa yang telah dilakukannya.
- **Make superadmin** — memberikan pintasan penuh. Gunakannya dengan berhemat dan jangan sekali-kali
  sebagai jalan pintas bagi kebenaran yang boleh anda berikan dengan betul.

## Menamatkan superadmin lalai {#handoff}

Akaun yang anda gunakan untuk log masuk kali pertama ialah superadmin **stock**, dan ia tidak patut
kekal dalam perkhidmatan.

Cipta akaun superadmin sebenar, sahkan ia berfungsi, kemudian lumpuhkan yang lalai — itulah
**bootstrap handoff**. Satah kawalan hanya mengingatkan anda tentang ini setelah superadmin sebenar
aktif, jadi gesaan itu muncul tepat pada masa ia selamat untuk dilakukan.

## Peranan {#roles}

Peranan ada dua jenis: peranan **built-in** yang tidak boleh dipadam, dan peranan **custom** yang
anda cipta.

Peranan baharu bermula sebagai **viewer**: baca sahaja pada armada dan pemberitahuan, serta akses
viewer pada setiap nod yang telah diambil setakat ini. Bermula daripada sesuatu yang munasabah dan
menyempitkannya lebih baik daripada bermula daripada kosong lalu menemui jurangnya satu aduan pada
satu masa.

Anda boleh menamakan semula peranan, dan **menyalin** satu — cara terpantas untuk menghasilkan "sama
seperti operator, tambah laporan" tanpa membinanya semula dengan tangan.

## Akses: ciri dahulu, laluan jika perlu {#access}

**Access** memberikan peranan keupayaan satah kawalan sebagai suis biasa — lihat armada dan kamera,
urus armada, pemberitahuan, ejen AI.

**Advanced** memberikan awalan laluan API dan kata kerja tertentu bagi apa-apa yang tidak diliputi
oleh suis itu. Kebanyakan peranan tidak pernah memerlukannya.

Dua peraturan mentadbir keseluruhan matriks:

- **Awalan padanan terpanjang menang.**
- **Tiada peraturan bermakna ditolak.** Peranan tanpa sebarang peraturan ditolak segalanya dan tidak
  melihat sebarang menu.

Tolak-secara-lalai inilah sebabnya keupayaan baharu yang dibawa oleh naik taraf tidak senyap-senyap
menjadi tersedia kepada semua orang.

**Superadmin memintas setiap pemeriksaan**, jadi matriksnya kosong secara reka bentuk — tiada apa
untuk dikonfigurasikan dan tiada apa untuk tersilap.

## Menu dan kebenaran ialah benda yang sama {#menus}

**Menu access** menogol bahagian navigasi yang boleh dilihat oleh sesuatu peranan, dan setiap suis
memberi atau menarik balik **GET pada laluan API bahagian tersebut**.

Ini berbaloi difahami dan bukan sekadar diimbas: tiada "kebenaran UI" yang berasingan. Nav dijana
daripada matriks yang sama yang mengawal API, jadi sesuatu peranan tidak mungkin melihat halaman
yang tidak boleh dipanggilnya, dan menyembunyikan menu bukanlah langkah keselamatan yang diletakkan
di atas langkah sebenar — ia *ialah* langkah sebenar itu.

Akibat praktikalnya: jika seseorang tidak nampak sesuatu halaman, berikan keupayaan itu dan bukan
mencari tetapan paparan, kerana tetapan itu tidak wujud.

## Akses nod ialah soalan yang berasingan {#node-access}

Matriks menentukan apa yang boleh dilakukan seseorang **pada satah kawalan ini**. Ia tidak
menentukan apa yang boleh dilakukannya **pada sesebuah nod**.

Akses nod diberikan setiap nod, pada salah satu tahap nod itu sendiri — viewer (baca sahaja),
operator (baca serta tulis terhad) atau admin (baca dan tulis). Peranan yang mengambil sesuatu nod
sentiasa mempunyai akses penuh kepadanya.

Bezakan kedua-duanya: sesuatu peranan boleh dibenarkan melihat bahawa sebuah nod wujud di sini
sambil tiada akses kepada perantinya sendiri, dan itu tepat sekali untuk seseorang yang memantau
armada tetapi tidak boleh mengkonfigurasi semula kamera.

## Apabila rakan sekerja tidak nampak sesuatu halaman atau menu {#troubleshooting}

Entri menu yang hilang bermakna kebenaran yang hilang — tiada tetapan paparan. Mengikut susunan
kebarangkalian:

1. **Mereka belum ada peranan** — skrin menunggu. Tetapkan satu.
2. **Peranan mereka tiada peraturan bagi laluan itu**, dan tiada peraturan bermakna ditolak.
3. **Suis menu bagi bahagian itu dimatikan**, yang sama sahaja dengan peraturan itu tiada.
4. **Yang kurang ialah akses nod, bukan akses satah kawalan** — mereka nampak armada tetapi tidak
   boleh memandu peranti itu.
5. **Akaun itu dilumpuhkan.**

Setiap satu perubahan ini direkodkan dalam [log audit](audit-log).
