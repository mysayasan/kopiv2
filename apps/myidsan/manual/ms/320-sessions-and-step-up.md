---
title: Sesi dan pengesahan bertingkat
category: security
categoryLabel: Keselamatan
summary: Di mana sesi sebenarnya berada, cara menamatkan satu sekarang, dan apa yang dilindungi oleh pengesahan semula.
order: 320
---

# Sesi dan pengesahan bertingkat

## Di mana sesi berada {#where}

Log masuk di sini bertahan tiga hari, supaya seseorang yang berpindah antara aplikasi tidak diminta
kata laluan semula. Itu menjadikan sesi itu sendiri berbaloi dilindungi, dan berbaloi untuk mampu
ditamatkan serta-merta.

Sesi itu **bukan** baris yang anda lihat dalam senarai. Ia berada dalam cache; baris pangkalan data
wujud supaya senarai itu boleh dilukis sama sekali.

```arch
title : Cache ialah sesi itu — jadual hanyalah indeksnya
ext browser : Pelayar orang itu, memegang tidak lebih daripada kuki sesi
box auth : Pemeriksaan yang berjalan pada setiap permintaan
store cache : Cache. Catatan ini ADALAH sesi itu — membuangnya ialah yang menamatkan satu.
store table : Pangkalan data. Satu baris setiap sesi, supaya senarai boleh dilukis. Baris sahaja bukan bukti sesi masih hidup.
box screen : Senarai sesi, dalam Profil dan pada skrin Pengguna
browser -> auth : Setiap permintaan, membawa kuki
auth -> cache : Adakah sesi ini masih ada di sini?
auth --> table : Kali terakhir dilihat, disegarkan sambil berjalan
screen -> table : Sesi mana yang dimiliki akaun ini?
screen -> cache : ...dan adakah setiap satunya benar-benar masih hidup?
```

Inilah sebabnya senarai itu boleh dipercayai pada satu arah yang penting. Baris hidup lebih lama
daripada catatan cachenya — sesi yang sekadar tamat tempoh meninggalkan barisnya — jadi setiap
penyenaraian diselaraskan terhadap cache dan apa-apa yang hilang dilaporkan sebagai **tamat**.
Menarik balik memadamkan catatan cache dahulu dan menanda baris kemudian: jika langkah kedua gagal,
sesi itu tetap mati, iaitu arah gagal yang selamat.

## Menamatkan sesi {#revoke}

- **Sesi anda sendiri**, di bawah **Profil**: setiap sesi menunjukkan alamat asalnya, pelayarnya,
  bila ia bermula dan bila ia terakhir dilihat. Sesi anda dilabelkan, jadi anda boleh menamatkan yang
  lain tanpa menamatkan yang sedang anda gunakan. **Log keluar di semua tempat lain** melakukannya
  sekali gus.
- **Sesi orang lain**, daripada skrin **Pengguna**. Inilah yang perlu dicapai apabila komputer riba
  hilang atau seseorang berhenti, dan ia berkuat kuasa pada permintaan mereka yang seterusnya — bukan
  di penghujung tiga hari.

Menamatkan sesi tidak melumpuhkan akaun. Jika akaun itu tidak sepatutnya kembali, tetapkannya tidak
aktif juga; jika tidak orang itu sekadar log masuk semula.

Kedua-duanya ditulis ke [log audit](audit-log), berasingan bagi satu sesi dan bagi kesemuanya.

## Pengesahan semula bertingkat {#stepup}

Sesi superadmin boleh menetapkan peranan, membersihkan faktor kedua sesiapa, mengeluarkan kata laluan
sementara bagi mana-mana akaun, dan mengeksport atau memulihkan keseluruhan stor identiti. Selama
tiga hari, atas dasar tidak lebih daripada sekeping kuki.

Pengesahan bertingkat menutup itu. Sebelum satu set kecil tindakan, pelayan meminta anda membuktikan
anda masih memegang kelayakan itu — kata laluan anda, ditambah kod jika anda ada faktor yang
didaftarkan. Ia **bukan** sesi kedua: ia tanda bertempoh pendek pada sesi yang anda sudah ada, jadi
seseorang yang hanya memegang kuki curi tidak boleh menghasilkannya.

Ia bertahan **lima minit**, cukup lama untuk menghabiskan satu kelompok kerja pentadbiran tanpa
menaip semula kata laluan setiap klik, dan cukup pendek supaya komputer riba yang ditinggalkan tidak
kekal ditingkatkan.

Hari ini ia diminta sebelum:

- mengeksport atau memulihkan sandaran,
- menyelesaikan permintaan set semula kata laluan (yang mengeluarkan kata laluan sementara),
- membersihkan faktor kedua akaun lain atau kunci keselamatannya.

Tanda itu diterbitkan daripada id sesi dan berada dalam cache bersamanya, jadi menarik balik sesi
turut membawa peningkatannya pergi, dan mula semula pelayan tidak meninggalkan sesiapa ditingkatkan.

Kedua-dua hasil diaudit — pengesahan semula yang berjaya dan yang gagal. Rentetan kegagalan terhadap
pengesahan bertingkat ialah rupa seseorang yang menduga sesi yang dicuri.

## Ke mana seterusnya {#next}

- [Log audit](audit-log) — sesi yang ditamatkan, dan pengesahan bertingkat yang dicuba.
- [Faktor kedua dan pemulihan akaun](second-factor) — apa yang diminta oleh pengesahan bertingkat.
- [Sandaran dan pemulihan](backup-restore) — tindakan yang paling ketat dikawal olehnya.
