---
title: Log audit
category: admin
categoryLabel: Pentadbiran
summary: Rekod tambah-sahaja bagi tindakan yang mungkin perlu dijelaskan oleh seseorang kemudian.
order: 520
---

# Log audit

**Audit Log** ialah rekod tambah-sahaja bagi tindakan sensitif pada satah kawalan: siapa membuat
apa, pada apa, dan sama ada ia berjaya.

Sifat tambah-sahaja itulah intinya. Ia bukan suapan untuk anda kemaskan — ia benda yang anda rujuk
apabila sebuah nod hilang, kebenaran berubah, atau seseorang bertanya apa yang berlaku pada hari
Selasa.

## Apa yang diberitahu oleh setiap baris {#columns}

| Lajur | Maksud |
|---|---|
| **Time** | Bila ia berlaku. |
| **Actor** | Akaun yang melakukannya, atau **System** bagi sesuatu yang tiada orang mencetuskannya. |
| **Action** | Apa yang cuba dilakukan. |
| **Target** | Kepada apa ia dilakukan — nod, pengguna, peranan. |
| **Outcome** | Berjaya, ditolak, atau ralat. |
| **Detail** | Butiran khusus yang berbaloi disimpan. |

## Percubaan yang ditolak turut direkodkan {#denied}

Lajur outcome mempunyai tiga nilai, dan **denied** ialah yang orang lupa untuk mencarinya.

Tindakan yang ditolak ialah bukti. Seseorang yang berulang kali mencuba sesuatu yang tidak
dibenarkan baginya ialah keadaan yang berbeza daripada seseorang yang melakukannya sekali dengan
jayanya, dan log yang hanya merekod kejayaan tidak akan dapat memberitahu anda yang mana satu sedang
anda lihat.

## Apa yang direkodkan {#actions}

Tindakan berbentuk armada:

- **Nod diambil**, **dilepaskan**, **menggugurkan diri**, dan arahan yang dihantar kepada nod.
- **Kunci armada diputar** — berbaloi diperhatikan, kerana ia mengubah peranti mana yang boleh
  ditemui sama sekali.

Tindakan berbentuk akses:

- **Peranan diubah**, **pengguna dihidupkan atau dilumpuhkan**, **pengguna dinaikkan** menjadi
  superadmin.
- **Akses nod diberikan** atau **ditarik balik**.

Serta tindakan pengurusan ejen AI sendiri — memasang atau mengimport model, menghalakan satah
kawalan ke sesuatu titik akhir, menjana ringkasan atas permintaan.

Senarai ini sengaja bukan "segala-galanya". Log bagi setiap bacaan ialah log yang tiada siapa
membacanya; inilah tindakan yang mengubah apa sistem ini atau siapa yang boleh menggunakannya.

## System sebagai pelaku {#system}

Sesetengah baris mempunyai **System** sebagai pelaku kerana tiada orang mencetuskannya — nod yang
menggugurkan dirinya, kerja berjadual, pemulihan automatik.

Itu bukan pengguna yang dilindungi identitinya. Ia bermaksud tindakan itu memang tiada manusia di
belakangnya, dan selalunya itulah yang perlu anda tetapkan.

## Menggunakannya {#using}

Bacalah apabila armada mengejutkan anda.

Nod yang hilang sama ada telah **dilepaskan** (ada pelaku, dengan sengaja), **menggugurkan diri**
(System, dari pihak nod itu sendiri) atau sekadar luar talian tanpa apa-apa dilog — dan ketiga-tiga
itu menunjuk ke langkah seterusnya yang sama sekali berbeza. Log mengubah "nod itu hilang" menjadi
soalan yang ada jawapannya.

Perkara yang sama terpakai pada kebenaran: "semalam saya boleh lihat ini" dijawab oleh perubahan
peranan yang ada nama dan cap masa padanya.

Salinan jejak audit disertakan dalam [laporan](reports) **Security & Access**, iaitu bentuk yang
sesuai diserahkan kepada sesiapa yang memerlukannya di atas kertas.
