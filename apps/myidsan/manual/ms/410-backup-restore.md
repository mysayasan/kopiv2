---
title: Sandaran dan pemulihan
category: operations
categoryLabel: Operasi
summary: Satu perkara yang perlu disediakan sebelum anda memerlukannya, dan apa yang sebenarnya diganti oleh pemulihan.
order: 410
---

# Sandaran dan pemulihan

MyIDSan ialah aplikasi yang kehilangan pangkalan datanya mengunci setiap orang keluar daripada setiap
aplikasi serentak. Ia memegang setiap akaun dan cincangan kata laluan, setiap peranan, setiap aplikasi
bergantung yang berdaftar, kunci peribadi pihak berkuasa sijil SSO, kesemua rahsia dua faktor, dan
kata laluan ikatan direktori.

Sandaran ialah satu fail `.idbackup`, disulitkan dengan frasa laluan yang anda pilih. **Sistem ›
Sandaran & pemulihan.**

## Ambil satu sekarang {#export}

Pilih frasa laluan sekurang-kurangnya dua belas aksara dan muat turun failnya. Frasa laluan itu ialah
**satu-satunya** perlindungan padanya dan tiada cara untuk memulihkan kandungannya tanpanya, jadi
simpan fail dan frasa laluan itu dengan selamat dan berasingan.

Mengeksport memerlukan anda membuktikan semula kelayakan anda sendiri dahulu — lihat
[pengesahan bertingkat](sessions-and-step-up#stepup). Mengeluarkan keseluruhan stor identiti daripada
pelayan bukan tindakan yang patut boleh dilakukan oleh kuki yang dicuri.

Kedua-dua eksport dan pemulihan ditulis ke [log audit](audit-log).

## Apa yang ikut, dan apa yang tidak {#contents}

Fail itu membawa perkara yang menjadikan pelayan ini *pelayan ini*:

- peranan dan kebenaran,
- akaun, kumpulan dan cincangan kata laluan,
- faktor kedua — rahsia pengesah, kod pemulihan dan kunci keselamatan,
- aplikasi berdaftar, dasar pengesahannya dan URI ubah halanya,
- konfigurasi direktori dan pemetaan kumpulannya,
- pihak berkuasa sijil SSO, sijil **dan** kunci peribadi.

Ia sengaja meninggalkan segala yang milik *hos* dan bukan pelayan: `config.json`, sijil TLS, kunci
penyulitan semasa-rehat, log permintaan dan log masa jalan, permintaan set semula kata laluan yang
tertunggak (memulihkan yang lapuk akan mengeluarkan kata laluan sementara yang tiada sesiapa minta),
dan sesi langsung. **Jejak audit juga tidak disertakan** — ia ada arkibnya sendiri; lihat
[log audit](audit-log#retention).

> [!IMPORTANT]
> Kerana `config.json` tiada dalam fail itu, pelayan yang dipulihkan bukan pelayan yang
> dikonfigurasikan. Simpan salinan konfigurasi juga, atau bersedia untuk mengulang dasar log masuk,
> port dan tetapan storan secara manual.

## Bagaimana rahsia bertahan sepanjang perpindahan {#secrets}

Inilah bahagian yang menentukan sama ada anda ada sandaran atau fail yang mati.

Rahsia dua faktor dan kata laluan ikatan direktori dimeterai pada cakera dengan kunci semasa-rehat
**hos ini**. Menyalin bait yang dimeterai itu ke dalam arkib akan menghasilkan sandaran yang
dipulihkan "dengan jayanya" ke hos baharu dan kemudian gagal setiap pemeriksaan faktor kedua — hasil
paling teruk yang mungkin, kerana tiada sesiapa akan tahu sehingga seseorang cuba log masuk.

Jadi ia **dibuka meterai semasa masuk ke arkib** (yang itu sendiri disulitkan dengan frasa laluan
anda) dan **dimeterai semula dengan kunci hos destinasi** semasa keluar. Kunci semasa-rehat itu
sendiri tidak pernah ada dalam fail. Rahsia bergerak di dalam arkib yang disulitkan; identiti mesin
tidak.

## Memulihkan {#restore}

**Buka dan periksa** dahulu. Manifes fail itu menyatakan versi mana yang menciptanya, bila, dan
bahagian mana yang dipegangnya — baca itu sebelum melaksanakan apa-apa.

Kemudian pilih apa yang berlaku kepada apa yang sudah ada di sini:

- **Ganti apa yang ada di sini** membersihkan rekod yang sepadan dahulu. Ini pilihan yang betul
  apabila membina semula pelayan yang hilang.
- **Kekalkan kedua-duanya** menambah rekod sandaran di samping yang sedia ada. Gunakannya untuk
  menggabungkan pendaftaran satu pelayan ke dalam pelayan lain, dan jangkakan anda perlu
  menyelaraskan pendua secara manual.

Memulihkan menggantikan akaun dan peranan pada pelayan ini **termasuk yang anda sedang gunakan untuk
log masuk**. Semua orang dilog keluar, termasuk anda, dan perlu log masuk semula dengan akaun daripada
sandaran itu. Jika sesetengah rekod merujuk kepada sesuatu yang tiada dalam pemulihan itu, ia
dilangkau dan disenaraikan dan bukannya digugurkan secara senyap.

Seperti eksport, pemulihan meminta anda membuktikan semula kelayakan anda dahulu.

## Membina semula pelayan yang hilang {#rebuild}

1. Pasang MyIDSan pada hos baharu dan biarkan ia bermula. Ia mencipta superadmin permulaannya sendiri
   — abaikannya; pemulihan itu akan menggantikan senarai akaun.
2. Pada skrin larian pertama, pilih **pulih daripada sandaran** dan bukannya melalui persediaan. Ia
   ditawarkan di situ atas sebab inilah.
3. Log masuk dengan akaun daripada sandaran itu.
4. Letakkan semula `config.json`, atau ulang tetapannya, dan mula semula.
5. Periksa aplikasi bergantung masih boleh melog masuk seseorang. Pihak berkuasa sijil SSO ikut
   bersama sandaran, jadi ia sepatutnya boleh — dan masa untuk mengetahuinya ialah sekarang, bukan
   pada hari bekerja berikutnya.

> [!NOTE]
> Jika hos baharu itu mempunyai alamat berbeza, URI ubah hala yang didaftarkan bagi aplikasi
> bergantung anda masih menunjuk ke yang lama. Ia dipadankan dengan tepat; lihat
> [Menyambung aplikasi](connecting-an-app#form).

## Ke mana seterusnya {#next}

- [Menyambung aplikasi](connecting-an-app) — memeriksa semula aplikasi bergantung selepas pembinaan
  semula.
- [Log audit](audit-log) — tempat eksport dan pemulihan direkodkan.
