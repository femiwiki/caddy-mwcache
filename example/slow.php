<?php
header("Cache-Control: s-maxage=5");
header("Date: " . date("D, j M Y G:i:s T"));
sleep(1);
echo "Hello, World<br>\n";
