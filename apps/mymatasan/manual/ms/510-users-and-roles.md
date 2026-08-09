---
title: Pengguna dan peranan
category: administration
categoryLabel: Pentadbiran
summary: Cipta akaun, pilih peranan yang betul, dan fahami apa yang dikuatkuasakan matriks kebenaran.
order: 510
---

# Pengguna dan peranan

Akaun log masuk tempatan berada dalam **Tetapan → Pengguna**. Setiap akaun mempunyai peranan, dan
peranan itu menentukan apa yang boleh dilakukannya.

## Tiga peranan {#roles}

| Peranan | Boleh | Tidak boleh |
|---|---|---|
| **Pemerhati** | Menonton video langsung. Melihat bahawa amaran telah dicetuskan. Menukar kata laluan sendiri. | Membuka rakaman. Mengakui, PTZ, cakap-balas. Apa-apa dalam Tetapan. |
| **Operator** | Semua di atas, tambah main balik dan muat turun rakaman, mencari penampakan objek, mengakui amaran, PTZ dan cakap-balas. | Memadam apa-apa. Menukar peraturan, kamera atau tetapan. Menguruskan pengguna. |
| **Pentadbir** | Segala-galanya. | — |

Akaun baharu adalah **operator** secara lalai. Pindahkan akaun kepada pemerhati apabila ia sepatutnya
hanya untuk menonton.

## Mengapa operator tidak boleh memadam {#evidentiary}

Garis di bawah operator ialah inti keseluruhan model ini: **operator yang berada di lokasi semasa
sesuatu insiden tidak boleh memusnahkan rakaman insiden itu.**

Itulah yang menjadikan perakam ini satu rekod dan bukan sekadar kemudahan. Ia bukan komen tentang
sesiapa yang anda ambil bekerja — ia sifat yang membolehkan rakaman itu dipercayai selepas kejadian,
termasuk oleh orang yang bertugas ketika itu.

Tahan godaan untuk menjadikan semua orang pentadbir kerana sesuatu kebenaran pernah menghalang sekali.
Setiap akaun yang boleh memadam rakaman ialah akaun yang menjadikan rakamannya sedikit kurang
bermakna.

## Ia dikuatkuasakan pada pelayan {#enforcement}

Setiap permintaan disemak terhadap peranan pengguna yang log masuk, bukan hanya yang menulis. Kawasan
yang dinafikan benar-benar tidak tersedia, bukan disembunyikan di sebalik URL yang boleh ditaip
seseorang.

Matriks ialah **nafi secara lalai**: apa-apa yang tidak diberikan akan ditolak. Pengguna tanpa peranan
yang ditetapkan tidak boleh melakukan apa-apa langsung, dan itulah satu-satunya bacaan yang selamat
bagi akaun yang mula disediakan seseorang dan tidak diselesaikan.

## Mencipta akaun {#creating}

Berikan nama pengguna, kata laluan dan peranan. Dua tabiat yang wajar dikekalkan:

- **Satu akaun bagi setiap orang.** Log masuk berkongsi memusnahkan nilai audit segala yang lain di
  sini.
- **Peranan terendah yang mampu melakukan tugas itu.** Naikkan pangkat apabila seseorang terkena
  dinding, dan bukan bermula dengan semua orang sebagai pentadbir.

Akaun boleh dinyahaktifkan dan bukan dipadam, dan itulah yang biasanya anda mahukan apabila seseorang
pergi — pemadaman membuang akaun itu, penyahaktifan mengekalkan nama itu terlekat pada sejarahnya.

## Kata laluan {#passwords}

Semua orang boleh menukar kata laluan sendiri daripada sesi mereka sendiri. Pentadbir boleh menetapkan
semula kata laluan orang lain.

Akaun admin permulaan sentiasa dipaksa menukar kata laluannya pada log masuk pertama — lihat
[Log masuk buat kali pertama](first-sign-in#change-password). Kata laluan yang ditetapkan semula
paling baik ditanda mesti-tukar supaya orang itu memilih kata laluannya sendiri.

Kuncian selepas kegagalan berulang berlaku secara automatik dan terpakai kepada semua orang, termasuk
pentadbir — lihat [Apabila log masuk dikunci](first-sign-in#lockout). Pentadbir tidak boleh
memendekkan kuncian orang lain, dan itu disengajakan.

## Matriks kebenaran {#matrix}

Di sebalik peranan itu ialah matriks yang sebenarnya dirujuk pelayan: satu baris bagi setiap kawasan
API yang ditadbir, dengan apa yang boleh dilakukan setiap peranan terbina dalam padanya.

Peranan terbina dalam disemai daripada katalog itu pada larian pertama. Menyunting matriks adalah
untuk tapak yang benar-benar memerlukan anak tangga yang tidak dinyatakan tiga peranan itu —
"operator di sini juga boleh membersihkan amaran diagnostik", contohnya. Ia kawalan keselamatan
sebenar, jadi ubahnya secara sengaja dan uji semula dengan akaun sebenar peranan itu selepasnya.

Dua kelakuan yang wajar diketahui:

- **Peraturan padanan paling khusus menang**, dan peraturan tidak bergabung. Itulah cara pemberian
  luas dikecualikan oleh penafian yang lebih sempit — tetapan boleh dibaca, tetapi pengguna di
  bawahnya tidak.
- **Lalai hanya dikenakan kepada peranan yang langsung tiada kebenaran.** Setelah anda menala sesuatu
  peranan, naik taraf tidak akan sekali-kali menetapkannya semula secara senyap.

## Log masuk bersekutu {#federated}

mymatasan mengesahkan terhadap akaun tempatannya sendiri. Ia tiada kaki log masuk tunggal — itu tugas
myidsan, dan myseliasan — jadi akaun di sini ialah akaunnya.

Kekalkan sekurang-kurangnya dua akaun pentadbir. Peranti yang satu-satunya pentadbirnya telah pergi,
dengan kata laluan yang tiada sesiapa tahu, hanya boleh dipulihkan dengan menetapkan semula log masuk
admin pada konsol.
